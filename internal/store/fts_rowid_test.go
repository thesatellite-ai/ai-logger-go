package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/khanakia/ai-logger/internal/store"
)

// TestFTSRowidMap_SurvivesMissingAndStaleRows: the entry→search-rowid map
// is a cache. Rows an older ailog wrote (no map row) and a map row that
// points at another entry must still update the right search row.
func TestFTSRowidMap_SurvivesMissingAndStaleRows(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "log.db")
	st, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	a, _ := st.InsertEntry(ctx, store.InsertEntryInput{SessionID: "s", Prompt: "alpha question"})
	b, _ := st.InsertEntry(ctx, store.InsertEntryInput{SessionID: "s", Prompt: "bravo question"})

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	// a: no map row (an older binary wrote it); b: map points at a's row.
	var aRow int64
	if err := db.QueryRow(`SELECT fts_rowid FROM entries_fts_rowid WHERE entry_id = ?`, a).Scan(&aRow); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM entries_fts_rowid WHERE entry_id = ?`, a); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE entries_fts_rowid SET fts_rowid = ? WHERE entry_id = ?`, aRow, b); err != nil {
		t.Fatal(err)
	}

	if err := st.AttachResponse(ctx, store.AttachResponseInput{EntryID: a, Response: "zulu answer"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AttachResponse(ctx, store.AttachResponseInput{EntryID: b, Response: "yankee answer"}); err != nil {
		t.Fatal(err)
	}
	for word, want := range map[string]string{"zulu": a, "yankee": b, "alpha": a, "bravo": b} {
		hits, err := st.Search(ctx, word, store.SearchFilter{})
		if err != nil || len(hits) != 1 || hits[0].ID != want {
			t.Fatalf("search %q = %v %v, want %s", word, hits, err, want)
		}
	}
	if err := st.Redact(ctx, a); err != nil {
		t.Fatal(err)
	}
	if hits, _ := st.Search(ctx, "zulu", store.SearchFilter{}); len(hits) != 0 {
		t.Fatal("redacted entry still matches")
	}
	if hits, _ := st.Search(ctx, "yankee", store.SearchFilter{}); len(hits) != 1 {
		t.Fatal("redact touched another entry's search row")
	}
}

// TestFTSRowidMap_FilledForOldDatabase: a database whose search rows were
// written before the map existed gets every row mapped on migration.
func TestFTSRowidMap_FilledForOldDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "log.db")
	st, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := st.InsertEntry(ctx, store.InsertEntryInput{SessionID: "s", Prompt: "charlie"})
	_ = st.Close()
	db, _ := sql.Open("sqlite", path)
	for _, q := range []string{`DROP TABLE entries_fts_rowid`, `UPDATE schema_meta SET version = 2 WHERE id = 1`} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.Close()
	st, err = store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	db, _ = sql.Open("sqlite", path)
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entries_fts_rowid WHERE entry_id = ?`, id).Scan(&n); err != nil || n != 1 {
		t.Fatalf("map row for an old entry: %d %v", n, err)
	}
}
