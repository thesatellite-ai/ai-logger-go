package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/khanakia/ai-logger/internal/store"
)

func TestTagsStarredNotes_RoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	id, err := s.InsertEntry(ctx, store.InsertEntryInput{Prompt: "q"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetTags(ctx, id, "foo,bar"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStarred(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNotes(ctx, id, "some personal note"); err != nil {
		t.Fatal(err)
	}
	e, err := s.GetByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if e.Tags != "foo,bar" || !e.Starred || e.Notes != "some personal note" {
		t.Fatalf("round trip failed: %+v", e)
	}
	hits, err := s.Search(ctx, "personal", store.SearchFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != id {
		t.Fatal("notes were not indexed in FTS5")
	}
}

func TestRedact_StripsBodiesKeepsMetadata(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	id, err := s.InsertEntry(ctx, store.InsertEntryInput{
		Prompt:   "my api key is sk-ant-example",
		Response: "ok",
		Project:  "github.com/x/y",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Redact(ctx, id); err != nil {
		t.Fatal(err)
	}
	e, err := s.GetByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if e.Prompt != "[redacted]" || e.Response != "[redacted]" {
		t.Fatalf("bodies not redacted: %+v", e)
	}
	if e.Project != "github.com/x/y" {
		t.Fatal("project metadata should survive redact")
	}
}

func TestPurgeBefore_DeletesOldEntries(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	for i := 0; i < 3; i++ {
		if _, err := s.InsertEntry(ctx, store.InsertEntryInput{Prompt: "q"}); err != nil {
			t.Fatal(err)
		}
	}
	future := time.Now().Add(time.Hour)
	n, err := s.PurgeBefore(ctx, future)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("expected 3 purged, got %d", n)
	}
	rows, err := s.Recent(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected empty after purge, got %d", len(rows))
	}
}

func TestTurnIndex_AutoComputedWithinSession(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	sid := "sess-aaaa"
	var ids []string
	for i := 0; i < 3; i++ {
		id, err := s.InsertEntry(ctx, store.InsertEntryInput{
			SessionID: sid,
			Prompt:    "turn",
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows, err := s.SessionEntries(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	for i, r := range rows {
		if r.TurnIndex != i {
			t.Fatalf("turn %d: got turn_index=%d want %d", i, r.TurnIndex, i)
		}
	}
	// Rows 1 and 2 should have parent_entry_id pointing at the previous id.
	if rows[1].ParentEntryID != rows[0].ID {
		t.Fatalf("parent chain broken at turn 1: got %q want %q", rows[1].ParentEntryID, rows[0].ID)
	}
	if rows[2].ParentEntryID != rows[1].ID {
		t.Fatalf("parent chain broken at turn 2: got %q want %q", rows[2].ParentEntryID, rows[1].ID)
	}
}

func TestStats_Counts(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_, _ = s.InsertEntry(ctx, store.InsertEntryInput{Tool: "claude-code", Project: "a", Prompt: "p"})
	_, _ = s.InsertEntry(ctx, store.InsertEntryInput{Tool: "claude-code", Project: "b", Prompt: "p"})
	id, _ := s.InsertEntry(ctx, store.InsertEntryInput{Tool: "cursor", Project: "a", Prompt: "p"})
	_ = s.SetStarred(ctx, id, true)

	st, err := s.ComputeStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Total != 3 || st.Starred != 1 {
		t.Fatalf("stats: %+v", st)
	}
	if st.ByTool["claude-code"] != 2 || st.ByTool["cursor"] != 1 {
		t.Fatalf("by tool: %+v", st.ByTool)
	}
	if st.ByProject["a"] != 2 || st.ByProject["b"] != 1 {
		t.Fatalf("by project: %+v", st.ByProject)
	}
}
