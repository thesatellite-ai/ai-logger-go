package store

import (
	"context"
	"database/sql"
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

func applyFTSMigration(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, ftsDDL); err != nil {
		return fmt.Errorf("create entries_fts: %w", err)
	}
	return nil
}

func ftsInsert(ctx context.Context, db *sql.DB, entryID, prompt, response, notes string) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO entries_fts(entry_id, prompt, response, notes) VALUES (?, ?, ?, ?)`,
		entryID, prompt, response, notes,
	)
	return err
}

func ftsUpdateResponse(ctx context.Context, db *sql.DB, entryID, response string) error {
	_, err := db.ExecContext(ctx,
		`UPDATE entries_fts SET response = ? WHERE entry_id = ?`,
		response, entryID,
	)
	return err
}

// ftsSearch returns entry ids matching the FTS5 query, most recent first.
// Callers must validate/escape untrusted input before calling.
func ftsSearch(ctx context.Context, db *sql.DB, query string, limit int) ([]string, error) {
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
