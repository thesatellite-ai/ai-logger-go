package store_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/khanakia/ai-logger/internal/store"
)

// parentIndexSQL counts the indexes on entries that cover parent_entry_id.
const parentIndexSQL = `SELECT COUNT(*) FROM sqlite_master
	WHERE type = 'index' AND tbl_name = 'entries' AND sql LIKE '%parent_entry_id%'`

// importEntryIndexSQL counts the indexes on import_lines that cover entry_id.
const importEntryIndexSQL = `SELECT COUNT(*) FROM sqlite_master
	WHERE type = 'index' AND tbl_name = 'import_lines' AND sql LIKE '%(entry_id)%'`

// oldSchemaVersion is the marker a database written before the
// parent_entry_id index carries.
const oldSchemaVersion = 3

// rawCount runs a COUNT query on the database file outside the Store.
func rawCount(t *testing.T, path, query string) int {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow(query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// rawExec runs statements on the database file outside the Store.
func rawExec(t *testing.T, path string, stmts ...string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

// An old log.db (schema marker 3, no index on entries.parent_entry_id or
// import_lines.entry_id) gains both on the next Open: without them every
// merge of ailog dedupe scans the whole entries and import_lines tables,
// and every session it checks scans import_lines.
func TestOpen_OldDatabaseGainsMergeIndexes(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "log.db")
	st, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	rawExec(t, path,
		`DROP INDEX entry_parent_entry_id`,
		`DROP INDEX import_lines_entry_idx`,
		`UPDATE schema_meta SET version = 3 WHERE id = 1`)
	if n := rawCount(t, path, parentIndexSQL) + rawCount(t, path, importEntryIndexSQL); n != 0 {
		t.Fatalf("setup: %d merge indexes left", n)
	}
	if v := rawCount(t, path, `SELECT version FROM schema_meta`); v != oldSchemaVersion {
		t.Fatalf("setup: version %d", v)
	}

	st, err = store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	if n := rawCount(t, path, parentIndexSQL); n != 1 {
		t.Fatalf("parent_entry_id indexes after open = %d, want 1", n)
	}
	if n := rawCount(t, path, importEntryIndexSQL); n != 1 {
		t.Fatalf("import_lines entry_id indexes after open = %d, want 1", n)
	}
	if v := rawCount(t, path, `SELECT version FROM schema_meta`); v != store.SchemaVersion {
		t.Fatalf("schema marker = %d, want %d", v, store.SchemaVersion)
	}
}

// InTx commits on success, rolls back every write (ent and raw SQL) on
// error, and refuses nesting.
func TestInTx_CommitRollbackNested(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	hook, _ := st.InsertEntry(ctx, store.InsertEntryInput{Tool: "claude-code", SessionID: "s", Prompt: "same"})
	imp, _ := st.InsertEntry(ctx, store.InsertEntryInput{Tool: "claude-code", SessionID: "s", Prompt: "same", Response: "ans"})
	_ = st.ImportLineRecord(ctx, "k", "/f", imp)

	boom := errors.New("boom")
	err := st.InTx(ctx, func(tx *store.Store) error {
		if err := tx.MergeInto(ctx, imp, hook); err != nil {
			return err
		}
		if err := tx.InTx(ctx, func(*store.Store) error { return nil }); !errors.Is(err, store.ErrNestedTx) {
			t.Errorf("nested InTx = %v, want ErrNestedTx", err)
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("InTx = %v, want boom", err)
	}
	if _, err := st.GetByID(ctx, imp); err != nil {
		t.Fatal("rolled-back merge deleted the imported row")
	}
	if e, _ := st.GetByID(ctx, hook); e.Response != "" {
		t.Fatalf("rolled-back merge filled the hook row: %q", e.Response)
	}
	if id, _, _ := st.ImportLineEntry(ctx, "k"); id != imp {
		t.Fatalf("rolled-back merge moved the import key to %q", id)
	}

	if err := st.InTx(ctx, func(tx *store.Store) error { return tx.MergeInto(ctx, imp, hook) }); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetByID(ctx, imp); err == nil {
		t.Fatal("committed merge kept the imported row")
	}
	if e, _ := st.GetByID(ctx, hook); e.Response != "ans" {
		t.Fatalf("committed merge response = %q", e.Response)
	}
}
