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

// TestSessionRows_ZonesAndLinks: hook rows are stamped in the local zone,
// imported rows in UTC; SessionRows orders them by instant, uses the
// transcript time when known, and marks rows an import key points at.
func TestSessionRows_ZonesAndLinks(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "log.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ist := time.FixedZone("IST", 5*3600+1800)
	hookAt := time.Date(2026, 9, 1, 19, 44, 56, 0, ist) // 14:14:56 UTC
	importAt := time.Date(2026, 9, 1, 14, 0, 0, 0, time.UTC)
	hook, _ := st.InsertEntry(ctx, store.InsertEntryInput{Tool: "claude-code", SessionID: "s1", Prompt: "late", CreatedAt: &hookAt})
	imp, _ := st.InsertEntry(ctx, store.InsertEntryInput{Tool: "claude-code", SessionID: "s1", Prompt: "early", CreatedAt: &importAt, TranscriptAt: &importAt})
	if _, err := st.InsertEntry(ctx, store.InsertEntryInput{Tool: "codex", SessionID: "s1", Prompt: "other tool"}); err != nil {
		t.Fatal(err)
	}
	if err := st.ImportLineRecord(ctx, "k1", "/f", imp); err != nil {
		t.Fatal(err)
	}
	rows, err := st.SessionRows(ctx, "claude-code", "s1")
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows = %+v, %v", rows, err)
	}
	if rows[0].ID != imp || rows[1].ID != hook || !rows[0].Linked || rows[1].Linked {
		t.Fatalf("order or links wrong: %+v", rows)
	}
	if rows[1].TranscriptAt != nil || !rows[1].At().Equal(hookAt) || !rows[0].At().Equal(importAt) {
		t.Fatalf("times: %+v", rows)
	}
}

func TestFillEmpty_NeverOverwrites(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "log.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	id, _ := st.InsertEntry(ctx, store.InsertEntryInput{Tool: "claude-code", SessionID: "s", Prompt: "q", PermissionMode: "plan", TokensIn: 5})
	at := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	changed, err := st.FillEmpty(ctx, id, store.FillInput{PermissionMode: "default", TokensIn: 9, TokensOut: 3, Response: "a", Model: "m", TranscriptAt: &at})
	if err != nil || !changed {
		t.Fatalf("fill: %v %v", changed, err)
	}
	e, _ := st.GetByID(ctx, id)
	if e.PermissionMode != "plan" || e.TokenCountIn != 5 || e.TokenCountOut != 3 || e.Response != "a" || e.Model != "m" || e.TranscriptAt == nil {
		t.Fatalf("entry = %+v", e)
	}
	if changed, _ := st.FillEmpty(ctx, id, store.FillInput{Response: "b", Model: "n"}); changed {
		t.Fatal("second fill overwrote")
	}
	hits, _ := st.Search(ctx, "a", store.SearchFilter{})
	if len(hits) == 0 {
		t.Fatal("filled response not searchable")
	}
}

func TestMergeIntoAndBackup(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "log.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	hook, _ := st.InsertEntry(ctx, store.InsertEntryInput{Tool: "claude-code", SessionID: "s", Prompt: "same", Terminal: "ghostty"})
	at := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	imp, _ := st.InsertEntry(ctx, store.InsertEntryInput{Tool: "claude-code", SessionID: "s", Prompt: "same", Response: "ans", Terminal: "x", CreatedAt: &at})
	child, _ := st.InsertEntry(ctx, store.InsertEntryInput{Tool: "claude-code", SessionID: "s", Prompt: "next", ParentEntryID: imp})
	_ = st.ImportLineRecord(ctx, "k", "/f", imp)
	if d, _ := st.DuplicateSessions(ctx); len(d) != 1 || d[0] != [2]string{"claude-code", "s"} {
		t.Fatalf("duplicate sessions = %v", d)
	}
	backup := filepath.Join(dir, "copy.db")
	if err := st.BackupTo(ctx, backup); err != nil {
		t.Fatal(err)
	}
	if err := st.BackupTo(ctx, backup); err == nil {
		t.Fatal("backup overwrote an existing file")
	}
	if err := st.MergeInto(ctx, imp, hook); err != nil {
		t.Fatal(err)
	}
	if err := st.MergeInto(ctx, hook, hook); err == nil {
		t.Fatal("merge into itself accepted")
	}
	e, _ := st.GetByID(ctx, hook)
	if e.Response != "ans" || e.Terminal != "ghostty" || e.TranscriptAt == nil || !e.TranscriptAt.Equal(at) {
		t.Fatalf("merged = %+v", e)
	}
	if id, ok, _ := st.ImportLineEntry(ctx, "k"); !ok || id != hook {
		t.Fatalf("key not moved: %q", id)
	}
	if c, _ := st.GetByID(ctx, child); c.ParentEntryID != hook {
		t.Fatalf("child parent = %q", c.ParentEntryID)
	}
	if _, err := st.GetByID(ctx, imp); err == nil {
		t.Fatal("merged row not deleted")
	}
	copyStore, err := store.Open(ctx, backup)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = copyStore.Close() }()
	if _, err := copyStore.GetByID(ctx, imp); err != nil {
		t.Fatal("backup does not hold the pre-merge rows")
	}
}
