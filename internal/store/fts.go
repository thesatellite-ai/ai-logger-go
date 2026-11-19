package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ftsDDL creates the FTS5 virtual table mirroring the searchable columns
// of `entries`. Standalone (not content=) to avoid an integer rowid
// dependency — we manage inserts/updates ourselves.
const ftsDDL = `
CREATE VIRTUAL TABLE IF NOT EXISTS entries_fts USING fts5(
    entry_id UNINDEXED,
    prompt,
    response,
    notes,
    tokenize = 'porter unicode61'
);
`

// ftsRowidDDL is the side index from an entry id to its entries_fts
// rowid. entries_fts.entry_id is UNINDEXED (an FTS5 column cannot carry
// a b-tree index), so "WHERE entry_id = ?" scans the whole index: about
// 70 ms per update on a 72k-entry database, which made an import that
// attaches tens of thousands of answers take most of an hour. The map is
// a cache: every lookup checks the rowid still holds that entry and
// falls back to the scan (and repairs the map) when it does not, so rows
// written by an older ailog binary, which never maintains it, stay
// correct.
const ftsRowidDDL = `
CREATE TABLE IF NOT EXISTS entries_fts_rowid (
    entry_id  TEXT PRIMARY KEY,
    fts_rowid INTEGER NOT NULL
);
`

func applyFTSMigration(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, ftsDDL); err != nil {
		return fmt.Errorf("create entries_fts: %w", err)
	}
	if _, err := db.ExecContext(ctx, ftsRowidDDL); err != nil {
		return fmt.Errorf("create entries_fts_rowid: %w", err)
	}
	// One scan fills the map for every row indexed so far.
	if _, err := db.ExecContext(ctx,
		`INSERT OR IGNORE INTO entries_fts_rowid (entry_id, fts_rowid) SELECT entry_id, rowid FROM entries_fts`); err != nil {
		return fmt.Errorf("fill entries_fts_rowid: %w", err)
	}
	return nil
}

// ftsRowid returns the entries_fts rowid of an entry: from the map when
// it still points at that entry, else by scanning (then the map is
// repaired). ok=false when the entry has no search row.
func ftsRowid(ctx context.Context, db querier, entryID string) (rowid int64, ok bool, err error) {
	err = db.QueryRowContext(ctx, `
		SELECT f.rowid FROM entries_fts_rowid m JOIN entries_fts f ON f.rowid = m.fts_rowid
		WHERE m.entry_id = ? AND f.entry_id = ?`, entryID, entryID).Scan(&rowid)
	if err == nil {
		return rowid, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}
	err = db.QueryRowContext(ctx, `SELECT rowid FROM entries_fts WHERE entry_id = ? LIMIT 1`, entryID).Scan(&rowid)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return rowid, true, ftsRemember(ctx, db, entryID, rowid)
}

// ftsRemember stores an entry's search rowid in the map.
func ftsRemember(ctx context.Context, db querier, entryID string, rowid int64) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO entries_fts_rowid (entry_id, fts_rowid) VALUES (?, ?)
		 ON CONFLICT(entry_id) DO UPDATE SET fts_rowid = excluded.fts_rowid`, entryID, rowid)
	return err
}

// ftsUpdate sets columns of an entry's search row (set is the SQL SET
// list, args its values); a no-op when the entry has no search row.
func ftsUpdate(ctx context.Context, db querier, entryID, set string, args ...any) error {
	rowid, ok, err := ftsRowid(ctx, db, entryID)
	if err != nil || !ok {
		return err
	}
	_, err = db.ExecContext(ctx, `UPDATE entries_fts SET `+set+` WHERE rowid = ?`, append(args, rowid)...)
	return err
}

// ftsDelete removes an entry's search row and its map row.
func ftsDelete(ctx context.Context, db querier, entryID string) error {
	rowid, ok, err := ftsRowid(ctx, db, entryID)
	if err != nil {
		return err
	}
	if ok {
		if _, err := db.ExecContext(ctx, `DELETE FROM entries_fts WHERE rowid = ?`, rowid); err != nil {
			return err
		}
	}
	_, err = db.ExecContext(ctx, `DELETE FROM entries_fts_rowid WHERE entry_id = ?`, entryID)
	return err
}

func ftsInsert(ctx context.Context, db querier, entryID, prompt, response, notes string) error {
	res, err := db.ExecContext(ctx,
		`INSERT INTO entries_fts(entry_id, prompt, response, notes) VALUES (?, ?, ?, ?)`,
		entryID, prompt, response, notes,
	)
	if err != nil {
		return err
	}
	rowid, err := res.LastInsertId()
	if err != nil {
		return err
	}
	return ftsRemember(ctx, db, entryID, rowid)
}

func ftsUpdateResponse(ctx context.Context, db querier, entryID, response string) error {
	return ftsUpdate(ctx, db, entryID, "response = ?", response)
}

// ftsSearch returns entry ids matching the FTS5 query, most recent first.
// Callers must validate/escape untrusted input before calling.
func ftsSearch(ctx context.Context, db querier, query string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.QueryContext(ctx, `
        SELECT e.id
          FROM entries_fts f
          JOIN entries e ON e.id = f.entry_id
         WHERE entries_fts MATCH ?
         ORDER BY e.created_at DESC
         LIMIT ?`,
		query, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("fts5 query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
