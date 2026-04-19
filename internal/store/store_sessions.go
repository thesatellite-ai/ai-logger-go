package store

import (
	"context"
	"time"

	"github.com/khanakia/ai-logger/ent"
	"github.com/khanakia/ai-logger/ent/entry"
)

// SessionSummary is one row of "give me every distinct session" — one
// entry per session_id with aggregated metadata for the listing page.
type SessionSummary struct {
	SessionID   string    `json:"session_id"`
	SessionName string    `json:"session_name"`
	Tool        string    `json:"tool"`
	Project     string    `json:"project"`
	StartedAt   time.Time `json:"started_at"`
	EndedAt     time.Time `json:"ended_at"`
	TurnCount   int       `json:"turn_count"`
}

// Sessions returns every distinct session in the DB, ordered by most
// recent activity first. Aggregated in Go (one full scan) — for our
// scale this is faster than juggling SQL group-by + json on SQLite.
func (s *Store) Sessions(ctx context.Context) ([]SessionSummary, error) {
	all, err := s.client.Entry.Query().
		Where(entry.SessionIDNEQ("")).
		Order(ent.Asc(entry.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	bySession := map[string]*SessionSummary{}
	for _, e := range all {
		s := bySession[e.SessionID]
		if s == nil {
			s = &SessionSummary{
				SessionID:   e.SessionID,
				SessionName: e.SessionName,
				Tool:        e.Tool,
				Project:     e.Project,
				StartedAt:   e.CreatedAt,
				EndedAt:     e.CreatedAt,
			}
			bySession[e.SessionID] = s
		}
		// Latest non-empty session_name wins (user can rename).
		if e.SessionName != "" {
			s.SessionName = e.SessionName
		}
		if e.CreatedAt.Before(s.StartedAt) {
			s.StartedAt = e.CreatedAt
		}
		if e.CreatedAt.After(s.EndedAt) {
			s.EndedAt = e.CreatedAt
		}
		s.TurnCount++
	}

	out := make([]SessionSummary, 0, len(bySession))
	for _, s := range bySession {
		out = append(out, *s)
	}
	// Most recent activity first.
	sortByEndedAtDesc(out)
	return out, nil
}

// sortByEndedAtDesc sorts in-place; small enough for an insertion sort
// to be plenty fast and to stay dependency-free.
func sortByEndedAtDesc(xs []SessionSummary) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j].EndedAt.After(xs[j-1].EndedAt); j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}
