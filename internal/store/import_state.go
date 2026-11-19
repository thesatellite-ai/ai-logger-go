package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Tables that back the importer:
//
//	import_state — per-transcript watermark. Lets `ailog import` skip
//	               files whose mtime+size haven't moved since the last
//	               successful run, and read a grown file from the cursor
//	               the last read returned (read_offset, agentio's opaque
//	               Offset) instead of from byte 0. Cheap on first runs (small file count
//	               for new transcripts), invaluable on huge ~/.claude
//	               trees where re-walking every line is the slow part.
//
//	import_lines — per-message dedup key. A key is either the SHA-256
//	               hex of the transcript record's bytes (every row
//	               imported so far; agentio's Turn.Raw is the same
//	               bytes), or "msg:<tool>:<conversation>:<message id>"
//	               for a vendor message id (agentio Turn.ID). Both keys
//	               of a message point at the same entry. Survives the
//	               fact that hooks overload the `entries.raw` column
//	               with the transcript_path while import historically
//	               stored a hash in the same column.
//
// import_lines_entry_idx serves the lookups by entry: a session's linked
// rows (Store.SessionRows) and Store.MergeInto moving an entry's keys.
// Without it each was a scan of every key (80k on a real log), once per
// session and once per merge. CREATE INDEX IF NOT EXISTS gives an older
// database the index on its next open.
//
// Both are raw SQL (CREATE IF NOT EXISTS) — they're internal bookkeeping
// for one CLI subcommand and don't need to live in the ent schema.
const importStateDDL = `
CREATE TABLE IF NOT EXISTS import_state (
    file_path        TEXT PRIMARY KEY,
    mtime_unix_nano  INTEGER NOT NULL,
    size             INTEGER NOT NULL,
    last_imported_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS import_lines (
    line_hash    TEXT PRIMARY KEY,
    file_path    TEXT NOT NULL,
    entry_id     TEXT NOT NULL,
    created_at   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS import_lines_file_idx ON import_lines(file_path);
CREATE INDEX IF NOT EXISTS import_lines_entry_idx ON import_lines(entry_id);
`

// importOffsetColumn is the import_state column holding the read cursor.
// Added after the table first shipped, so ensureImportTables adds it to
// an older database (default 0 = read from the start, the old behaviour).
const importOffsetColumn = "read_offset"

// ensureImportTables creates the importer bookkeeping tables and adds any
// column a database made by an older ailog lacks. Idempotent; additive
// only, so an existing log.db keeps every row.
func ensureImportTables(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, importStateDDL); err != nil {
		return fmt.Errorf("create import tables: %w", err)
	}
	has, err := hasColumn(ctx, db, "import_state", importOffsetColumn)
	if err != nil {
		return fmt.Errorf("inspect import_state: %w", err)
	}
	if !has {
		if _, err := db.ExecContext(ctx,
			`ALTER TABLE import_state ADD COLUMN `+importOffsetColumn+` INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("add import_state.%s: %w", importOffsetColumn, err)
		}
	}
	return nil
}

// hasColumn reports whether table has column (PRAGMA table_info).
func hasColumn(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	found := false
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if name == column {
			found = true
		}
	}
	return found, rows.Err()
}

// FileWatermark is one import_state row.
type FileWatermark struct {
	MtimeUnixNano int64
	Size          int64
	// Offset is the read cursor the last import stored for the file
	// (0 for rows written before the column existed).
	Offset int64
	LastAt time.Time
}

// ImportFileWatermark returns the stored watermark for key (a transcript
// path or a database session key); ok=false when none.
func (s *Store) ImportFileWatermark(ctx context.Context, key string) (w FileWatermark, ok bool, err error) {
	var lastAt string
	row := s.q.QueryRowContext(ctx,
		`SELECT mtime_unix_nano, size, `+importOffsetColumn+`, last_imported_at FROM import_state WHERE file_path = ?`,
		key)
	if err = row.Scan(&w.MtimeUnixNano, &w.Size, &w.Offset, &lastAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return FileWatermark{}, false, nil
		}
		return FileWatermark{}, false, err
	}
	w.LastAt, _ = time.Parse(time.RFC3339Nano, lastAt)
	return w, true, nil
}

// ImportFileMark stores the watermark and read cursor for key.
func (s *Store) ImportFileMark(ctx context.Context, key string, mtime time.Time, size, offset int64) error {
	_, err := s.q.ExecContext(ctx, `
		INSERT INTO import_state (file_path, mtime_unix_nano, size, `+importOffsetColumn+`, last_imported_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(file_path) DO UPDATE SET
			mtime_unix_nano  = excluded.mtime_unix_nano,
			size             = excluded.size,
			`+importOffsetColumn+` = excluded.`+importOffsetColumn+`,
			last_imported_at = excluded.last_imported_at
	`, key, mtime.UnixNano(), size, offset, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// ImportLineExists reports whether a dedup key is recorded.
func (s *Store) ImportLineExists(ctx context.Context, key string) (bool, error) {
	_, ok, err := s.ImportLineEntry(ctx, key)
	return ok, err
}

// ImportLineEntry returns the entry a dedup key points at; ok=false when
// the key is not recorded (or empty).
func (s *Store) ImportLineEntry(ctx context.Context, key string) (entryID string, ok bool, err error) {
	if key == "" {
		return "", false, nil
	}
	row := s.q.QueryRowContext(ctx, `SELECT entry_id FROM import_lines WHERE line_hash = ?`, key)
	if err := row.Scan(&entryID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return entryID, true, nil
}

// ImportLineRecord records a dedup key for an entry; a key already
// recorded keeps its first entry.
func (s *Store) ImportLineRecord(ctx context.Context, key, source, entryID string) error {
	if key == "" {
		return nil
	}
	_, err := s.q.ExecContext(ctx, `
		INSERT OR IGNORE INTO import_lines (line_hash, file_path, entry_id, created_at)
		VALUES (?, ?, ?, ?)
	`, key, source, entryID, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
