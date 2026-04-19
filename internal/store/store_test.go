package store_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/khanakia/ai-logger/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestRoundTrip_InsertAndFTS5Search(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	cases := []store.InsertEntryInput{
		{Tool: "claude-code", Project: "ai-logger", Prompt: "fix the race in worker goroutine pool"},
		{Tool: "claude-code", Project: "ai-logger", Prompt: "add FTS5 migration to the store"},
		{Tool: "cursor", Project: "other", Prompt: "explain kubernetes pod lifecycle"},
	}

	var ids []string
	for _, in := range cases {
		id, err := s.InsertEntry(ctx, in)
		if err != nil {
			t.Fatalf("insert %q: %v", in.Prompt, err)
		}
		if len(id) != 36 {
			t.Fatalf("expected 36-char uuid, got %q", id)
		}
		ids = append(ids, id)
	}

	hits, err := s.Search(ctx, "race", store.SearchFilter{})
	if err != nil {
		t.Fatalf("search 'race': %v", err)
	}
	if len(hits) != 1 || hits[0].Prompt != cases[0].Prompt {
		t.Fatalf("expected 1 hit for 'race', got %d", len(hits))
	}

	hits, err = s.Search(ctx, "FTS5 OR kubernetes", store.SearchFilter{})
	if err != nil {
		t.Fatalf("search boolean: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits for 'FTS5 OR kubernetes', got %d", len(hits))
	}

	hits, err = s.Search(ctx, "FTS5 OR kubernetes", store.SearchFilter{Project: "ai-logger"})
	if err != nil {
		t.Fatalf("search with project filter: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("project filter: expected 1, got %d", len(hits))
	}
}

func TestAttachResponse_UpdatesEntryAndFTS(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	id, err := s.InsertEntry(ctx, store.InsertEntryInput{
		Tool:   "claude-code",
		Prompt: "what is entropy",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.AttachResponse(ctx, store.AttachResponseInput{
		EntryID:   id,
		Response:  "a measure of disorder in a thermodynamic system",
		Model:     "claude-opus-4-7",
		TokensOut: 42,
	}); err != nil {
		t.Fatalf("attach: %v", err)
	}

	got, err := s.GetByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Response == "" {
		t.Fatal("response not persisted")
	}
	if got.Model != "claude-opus-4-7" {
		t.Fatalf("model not persisted, got %q", got.Model)
	}
	if got.TokenCountOut != 42 {
		t.Fatalf("token count not persisted, got %d", got.TokenCountOut)
	}

	// Response text should now be searchable via FTS5.
	hits, err := s.Search(ctx, "thermodynamic", store.SearchFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != id {
		t.Fatalf("response text not indexed in FTS5")
	}
}

func TestResolveIDPrefix(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	id1, err := s.InsertEntry(ctx, store.InsertEntryInput{Prompt: "first"})
	if err != nil {
		t.Fatal(err)
	}

	full, err := s.ResolveIDPrefix(ctx, id1[:8])
	if err != nil {
		t.Fatalf("resolve prefix: %v", err)
	}
	if full != id1 {
		t.Fatalf("prefix resolved to wrong id: got %q want %q", full, id1)
	}

	if _, err := s.ResolveIDPrefix(ctx, "zzzzzzzz"); err == nil {
		t.Fatal("expected error for non-existent prefix")
	}
}

func TestRecent_OrdersByCreatedAtDesc(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	for i := 0; i < 5; i++ {
		if _, err := s.InsertEntry(ctx, store.InsertEntryInput{Prompt: "p"}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.Recent(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3, got %d", len(rows))
	}
	for i := 1; i < len(rows); i++ {
		if rows[i-1].CreatedAt.Before(rows[i].CreatedAt) {
			t.Fatal("recent rows not sorted desc by created_at")
		}
	}
}
