package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/khanakia/ai-logger/internal/store"
)

// TestImportState_MigratesOldTable opens a log.db whose import_state was
// created by an older ailog (no read_offset column): the column is added,
// the old watermark row is kept and reads with cursor 0 (read from the
// start, the old behaviour), and a second open changes nothing.
func TestImportState_MigratesOldTable(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "log.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE import_state (file_path TEXT PRIMARY KEY, mtime_unix_nano INTEGER NOT NULL, size INTEGER NOT NULL, last_imported_at TEXT NOT NULL)`,
		`INSERT INTO import_state VALUES ('/home/alice/.claude/projects/x.jsonl', 42, 1000, '2026-01-01T00:00:00Z')`,
		`CREATE TABLE import_lines (line_hash TEXT PRIMARY KEY, file_path TEXT NOT NULL, entry_id TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`INSERT INTO import_lines VALUES ('abc', '/home/alice/.claude/projects/x.jsonl', 'entry-1', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.Close()

	for round := 0; round < 2; round++ {
		st, err := store.Open(ctx, path)
		if err != nil {
			t.Fatalf("open round %d: %v", round, err)
		}
		wm, ok, err := st.ImportFileWatermark(ctx, "/home/alice/.claude/projects/x.jsonl")
		if err != nil || !ok || wm.MtimeUnixNano != 42 || wm.Size != 1000 {
			t.Fatalf("round %d: old watermark = %+v %v %v", round, wm, ok, err)
		}
		if round == 0 && wm.Offset != 0 {
			t.Fatalf("old row cursor = %d, want 0", wm.Offset)
		}
		id, ok, err := st.ImportLineEntry(ctx, "abc")
		if err != nil || !ok || id != "entry-1" {
			t.Fatalf("old dedup key lost: %q %v %v", id, ok, err)
		}
		if round == 0 {
			if err := st.ImportFileMark(ctx, "/home/alice/.claude/projects/x.jsonl", time.Unix(0, 42), 1000, 777); err != nil {
				t.Fatal(err)
			}
		} else if wm.Offset != 777 {
			t.Fatalf("cursor not kept across opens: %d", wm.Offset)
		}
		_ = st.Close()
	}
}

func TestImportLines_Keys(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "log.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if _, ok, err := st.ImportLineEntry(ctx, ""); ok || err != nil {
		t.Fatal("empty key matched")
	}
	if err := st.ImportLineRecord(ctx, "k", "/f", "e1"); err != nil {
		t.Fatal(err)
	}
	if err := st.ImportLineRecord(ctx, "k", "/f", "e2"); err != nil {
		t.Fatal(err)
	}
	if id, ok, _ := st.ImportLineEntry(ctx, "k"); !ok || id != "e1" {
		t.Fatalf("first entry not kept: %q", id)
	}
	if ok, _ := st.ImportLineExists(ctx, "missing"); ok {
		t.Fatal("missing key exists")
	}
}

func TestLatestTurnAndFindImported(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "log.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	t0 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	ins := func(prompt string, at time.Time) string {
		id, err := st.InsertEntry(ctx, store.InsertEntryInput{Tool: "codex", SessionID: "s1", Prompt: prompt, CreatedAt: &at})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	a := ins("first", t0)
	b := ins("second", t0.Add(time.Minute))
	if err := st.AttachResponse(ctx, store.AttachResponseInput{EntryID: a, Response: "one"}); err != nil {
		t.Fatal(err)
	}
	if id, resp, ok, _ := st.LatestTurn(ctx, "codex", "s1", t0.Add(30*time.Second)); !ok || id != a || resp != "one" {
		t.Fatalf("latest before b = %q %q %v", id, resp, ok)
	}
	if id, resp, ok, _ := st.LatestTurn(ctx, "codex", "s1", time.Time{}); !ok || id != b || resp != "" {
		t.Fatalf("latest = %q %q %v", id, resp, ok)
	}
	if _, _, ok, _ := st.LatestTurn(ctx, "claude-code", "s1", time.Time{}); ok {
		t.Fatal("another tool's session matched")
	}
	if _, _, ok, _ := st.LatestTurn(ctx, "codex", "s1", t0.Add(-time.Hour)); ok {
		t.Fatal("an entry newer than the answer matched")
	}
	if id, ok, _ := st.FindImportedEntry(ctx, store.ImportedTurn{Tool: "codex", SessionID: "s1", CreatedAt: t0, Prompt: "first"}); !ok || id != a {
		t.Fatalf("find = %q %v", id, ok)
	}
	for _, miss := range []store.ImportedTurn{
		{Tool: "codex", SessionID: "s1", CreatedAt: t0, Prompt: "other"},
		{Tool: "codex", SessionID: "s1", CreatedAt: t0.Add(time.Second), Prompt: "first"},
		{Tool: "codex", SessionID: "", CreatedAt: t0, Prompt: "first"},
		{Tool: "codex", SessionID: "s1", Prompt: "first"},
	} {
		if _, ok, _ := st.FindImportedEntry(ctx, miss); ok {
			t.Errorf("matched %+v", miss)
		}
	}
}
