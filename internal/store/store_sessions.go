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

// BrowseSessionsInput drives the /sessions page: a free-text query
// matched against session_id + session_name + tool, plus sort + pager.
//
// Query is a case-insensitive substring match applied post-aggregation.
// Sort accepts: "ended_at" (default), "started_at", "turn_count",
// "session_name". Dir: "asc" | "desc" (default desc).
type BrowseSessionsInput struct {
	Query  string
	Sort   string
	Dir    string
	Limit  int
	Offset int
}

// BrowseSessions returns a filtered + sorted + paginated session list
// along with the matching total. Aggregation happens in Go (same as
// Sessions()); we don't push this down to SQL because each session's
// row count / last-activity requires grouping all entries anyway.
func (s *Store) BrowseSessions(ctx context.Context, in BrowseSessionsInput) ([]SessionSummary, int, error) {
	all, err := s.Sessions(ctx)
	if err != nil {
		return nil, 0, err
	}

	// Filter (case-insensitive substring on id | name | tool).
	if q := trimLower(in.Query); q != "" {
		filtered := all[:0]
		for _, s := range all {
			if containsFold(s.SessionID, q) || containsFold(s.SessionName, q) || containsFold(s.Tool, q) {
				filtered = append(filtered, s)
			}
		}
		all = filtered
	}

	// Sort.
	sortSessions(all, in.Sort, in.Dir)

	total := len(all)

	// Paginate.
	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := in.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return nil, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return all[offset:end], total, nil
}

// sortSessions orders a slice in-place per the input. Default is
// ended_at desc (latest activity first).
func sortSessions(xs []SessionSummary, sortKey, dir string) {
	if sortKey == "" {
		sortKey = "ended_at"
	}
	asc := dir == "asc"
	less := func(a, b SessionSummary) bool {
		switch sortKey {
		case "started_at":
			return a.StartedAt.Before(b.StartedAt)
		case "turn_count":
			return a.TurnCount < b.TurnCount
		case "session_name":
			return a.SessionName < b.SessionName
		default: // ended_at
			return a.EndedAt.Before(b.EndedAt)
		}
	}
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0; j-- {
			// "out of order" depends on direction. Use strict less in
			// both branches so equal keys don't swap (stability).
			var swap bool
			if asc {
				swap = less(xs[j], xs[j-1])
			} else {
				swap = less(xs[j-1], xs[j])
			}
			if !swap {
				break
			}
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}

// containsFold is a lowercase substring match, ASCII-aware. Good
// enough for session ids (UUIDs), names (user-entered), and tool
// values — no need to pull in the unicode package.
func containsFold(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	return indexFold(haystack, needle) >= 0
}

func indexFold(haystack, needle string) int {
	if len(needle) > len(haystack) {
		return -1
	}
	hl, nl := len(haystack), len(needle)
	for i := 0; i+nl <= hl; i++ {
		match := true
		for j := 0; j < nl; j++ {
			if toLowerByte(haystack[i+j]) != toLowerByte(needle[j]) {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func toLowerByte(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + 32
	}
	return b
}

func trimLower(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	out := make([]byte, end-start)
	for i := 0; i < end-start; i++ {
		out[i] = toLowerByte(s[start+i])
	}
	return string(out)
}
