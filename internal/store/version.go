package store

import (
	"context"
	"database/sql"
	"fmt"
)

// SchemaVersion is the compile-time marker for what columns / indexes /
// tables this binary expects. Bump this integer whenever you add /
// remove a column, add an index, or change the FTS5 shape.
//
// How it's used:
//
//	Open() reads the stored version from the `schema_meta` table with a
//	single indexed SELECT. If it equals SchemaVersion, the migration
//	step is SKIPPED — saves the ~few-ms schema-inspection overhead
//	ent's Schema.Create does on every call.
//
//	If it doesn't match (or the table doesn't exist), ent + FTS5
//	migrations run, then the marker is bumped. Next time Open() is
//	called, we're back on the fast path.
//
// If you add a column to ent/schema/entry.go, bump this constant in
// the same commit. `ailog migrate` also always runs unconditionally,
// so users can force a re-migration even if the marker is already at
// the current version.
const SchemaVersion = 2

// currentSchemaVersion returns the version stored in the schema_meta
// table, or 0 if the table doesn't exist yet (fresh DB).
//
// Important: we swallow "no such table" errors because that's the
// expected state on a brand-new DB — Schema.Create will create the
// table on first run.
func currentSchemaVersion(ctx context.Context, db *sql.DB) int {
	var v int
	row := db.QueryRowContext(ctx, `SELECT version FROM schema_meta WHERE id = 1`)
	if err := row.Scan(&v); err != nil {
		return 0 // table missing or empty — treat as pre-migration
	}
	return v
}

// ensureSchemaMetaTable creates the `schema_meta` singleton table if
// it's missing. Idempotent — safe to call on every migrate.
func ensureSchemaMetaTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_meta (
			id      INTEGER PRIMARY KEY CHECK (id = 1),
			version INTEGER NOT NULL DEFAULT 0
		)`)
	if err != nil {
		return fmt.Errorf("create schema_meta: %w", err)
	}
	_, err = db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_meta (id, version) VALUES (1, 0)`)
	return err
}

// writeSchemaVersion bumps the stored marker to v.
func writeSchemaVersion(ctx context.Context, db *sql.DB, v int) error {
	_, err := db.ExecContext(ctx, `UPDATE schema_meta SET version = ? WHERE id = 1`, v)
	return err
}
