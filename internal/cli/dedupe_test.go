package cli_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/khanakia/ai-logger/internal/store"
)

func TestDedupeCommand_DryRunThenBackupAndMerge(t *testing.T) {
	db := isolatedHome(t)
	ctx := context.Background()
	st, err := store.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	hookAt := time.Date(2026, 3, 1, 15, 30, 0, 100e6, time.FixedZone("IST", 5*3600+1800))
	importAt := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	hook, _ := st.InsertEntry(ctx, store.InsertEntryInput{Tool: "claude-code", SessionID: "s1", Prompt: "Explain goroutines.", CreatedAt: &hookAt})
	imp, _ := st.InsertEntry(ctx, store.InsertEntryInput{Tool: "claude-code", SessionID: "s1", Prompt: "Explain goroutines.", Response: "Threads.", CreatedAt: &importAt})
	_ = st.ImportLineRecord(ctx, "k1", "/t.jsonl", imp)
	_ = st.Close()

	out, _, err := runCLI(t, "", "dedupe", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "claude-code: would merge 1") {
		t.Fatalf("dry run: %s", out)
	}
	if bs, _ := filepath.Glob(db + ".bak-*"); len(bs) != 0 {
		t.Fatal("dry run wrote a backup")
	}
	if len(lastEntries(t, 5)) != 2 {
		t.Fatal("dry run changed rows")
	}

	out, _, err = runCLI(t, "", "dedupe")
	if err != nil {
		t.Fatal(err)
	}
	bs, _ := filepath.Glob(db + ".bak-*")
	if len(bs) != 1 || !strings.Contains(out, "backup: "+bs[0]) || !strings.Contains(out, "claude-code: merged 1") {
		t.Fatalf("run: %s (backups %v)", out, bs)
	}
	es := lastEntries(t, 5)
	if len(es) != 1 || es[0]["id"] != hook || es[0]["response"] != "Threads." {
		t.Fatalf("after dedupe = %v", es)
	}
	bk, err := store.Open(ctx, bs[0])
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = bk.Close() }()
	if _, err := bk.GetByID(ctx, imp); err != nil {
		t.Fatal("backup lacks the merged row")
	}
}
