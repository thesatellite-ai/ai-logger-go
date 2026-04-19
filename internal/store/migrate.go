package store

import (
	"bytes"
	"context"
	"fmt"
	"io"
)

// MigrateDiff returns the DDL that MigrateApply would execute, WITHOUT
// running it. Use this for `ailog migrate --dry-run` previews.
//
// The output is the additive change-set only — ent's auto-migrate
// doesn't drop columns or tables by default (deliberate: old binaries
// reading a newer DB still work).
//
// We also emit the FTS5 virtual-table DDL at the end so a single
// dry-run shows the full migration state.
func (s *Store) MigrateDiff(ctx context.Context) (string, error) {
	var buf bytes.Buffer
	if err := s.client.Schema.WriteTo(ctx, &buf); err != nil {
		return "", fmt.Errorf("diff: %w", err)
	}
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "-- FTS5 virtual table (raw SQL, idempotent):")
	fmt.Fprint(&buf, ftsDDL)
	return buf.String(), nil
}

// MigrateApply runs ent + FTS5 migrations unconditionally and bumps
// the schema_meta version marker so the next Open() can take the fast
// path. Safe on an up-to-date DB (all steps are idempotent).
//
// Open() skips migrations when schema_meta.version already equals the
// compile-time SchemaVersion. `ailog migrate` calls this method
// directly, so it always runs the full check even when the marker says
// "already current" — useful as a forced repair.
func (s *Store) MigrateApply(ctx context.Context) error {
	if err := s.client.Schema.Create(ctx); err != nil {
		return fmt.Errorf("ent migrate: %w", err)
	}
	if err := applyFTSMigration(ctx, s.db); err != nil {
		return fmt.Errorf("fts5 migrate: %w", err)
	}
	if err := ensureSchemaMetaTable(ctx, s.db); err != nil {
		return fmt.Errorf("schema_meta: %w", err)
	}
	if err := writeSchemaVersion(ctx, s.db, SchemaVersion); err != nil {
		return fmt.Errorf("write version marker: %w", err)
	}
	return nil
}

// SchemaInspect dumps the current column list of the `entries` table
// to a writer. Diagnostic — confirms a new column really lives in the
// DB without shelling out to sqlite3.
func (s *Store) SchemaInspect(ctx context.Context, w io.Writer) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info("entries")`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	fmt.Fprintln(w, "entries columns:")
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    any
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		fmt.Fprintf(w, "  %-28s  %s\n", name, ctype)
	}
	return rows.Err()
}
