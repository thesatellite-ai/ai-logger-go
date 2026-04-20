package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Tables that back the importer:
//
//	import_state — per-source-file watermark. Lets `ailog import` skip
//	               files whose mtime+size haven't moved since the last
//	               successful run. Cheap on first runs (small file count
//	               for new transcripts), invaluable on huge ~/.claude
//	               trees where re-walking every line is the slow part.
//
//	import_lines — per-line dedup key. Each parsed transcript line gets
//	               a SHA-256 hash; if it's already here, we skip the
//	               write. Survives the fact that hooks overload the
//	               `entries.raw` column with the transcript_path while
//	               import historically stored a hash in the same column.
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
`

// ensureImportTables creates the importer bookkeeping tables. Idempotent.
func ensureImportTables(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, importStateDDL)
	if err != nil {
		return fmt.Errorf("create import tables: %w", err)
	}
	return nil
}

// FileWatermark is what's stored per source file. Zero values mean
// "never imported" (use ImportFileWatermark's ok return to disambiguate).
type FileWatermark struct {
	MtimeUnixNano int64
	Size          int64
	LastAt        time.Time
}

// ImportFileWatermark returns the last-recorded watermark for a source
// file. ok=false means the file has never been processed.
func (s *Store) ImportFileWatermark(ctx context.Context, filePath string) (w FileWatermark, ok bool, err error) {
	var lastAt string
	row := s.db.QueryRowContext(ctx,
		`SELECT mtime_unix_nano, size, last_imported_at FROM import_state WHERE file_path = ?`,
		filePath)
	if err = row.Scan(&w.MtimeUnixNano, &w.Size, &lastAt); err != nil {
		if err == sql.ErrNoRows {
			return FileWatermark{}, false, nil
		}
		return FileWatermark{}, false, err
	}
	w.LastAt, _ = time.Parse(time.RFC3339Nano, lastAt)
	return w, true, nil
}

// ImportFileMark upserts the watermark for a source file. Call this
// after a successful per-file import pass.
func (s *Store) ImportFileMark(ctx context.Context, filePath string, mtime time.Time, size int64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO import_state (file_path, mtime_unix_nano, size, last_imported_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(file_path) DO UPDATE SET
			mtime_unix_nano = excluded.mtime_unix_nano,
			size            = excluded.size,
			last_imported_at = excluded.last_imported_at
	`, filePath, mtime.UnixNano(), size, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// ImportLineExists reports whether a transcript line with this hash has
// already produced an entry — the per-line idempotency primitive.
func (s *Store) ImportLineExists(ctx context.Context, lineHash string) (bool, error) {
	if lineHash == "" {
		return false, nil
	}
	var n int
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM import_lines WHERE line_hash = ?`, lineHash)
	if err := row.Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// ImportLineRecord pins (line_hash → entry_id) so a future import pass
// can short-circuit. Call after the entry is successfully inserted.
func (s *Store) ImportLineRecord(ctx context.Context, lineHash, filePath, entryID string) error {
	if lineHash == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO import_lines (line_hash, file_path, entry_id, created_at)
		VALUES (?, ?, ?, ?)
	`, lineHash, filePath, entryID, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
