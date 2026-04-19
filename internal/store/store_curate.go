package store

import (
	"context"
	"time"

	"github.com/khanakia/ai-logger/ent/entry"
)

// RenameSession sets session_name on every entry that shares the given
// session id. Returns the number of rows updated.
func (s *Store) RenameSession(ctx context.Context, sessionID, name string) (int, error) {
	return s.client.Entry.Update().
		Where(entry.SessionIDEQ(sessionID)).
		SetSessionName(name).
		Save(ctx)
}

// SetStarred toggles the starred flag on a single entry.
func (s *Store) SetStarred(ctx context.Context, id string, starred bool) error {
	_, err := s.client.Entry.UpdateOneID(id).SetStarred(starred).Save(ctx)
	return err
}

// SetTags replaces (not merges) the tags csv on an entry. Tag merging
// is the CLI's job — see internal/cli/tag.go mergeTags.
func (s *Store) SetTags(ctx context.Context, id, csv string) error {
	_, err := s.client.Entry.UpdateOneID(id).SetTags(csv).Save(ctx)
	return err
}

// SetNotes replaces the free-form notes on an entry and re-syncs the
// FTS5 index so notes participate in `ailog search`.
func (s *Store) SetNotes(ctx context.Context, id, notes string) error {
	if _, err := s.client.Entry.UpdateOneID(id).SetNotes(notes).Save(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE entries_fts SET notes = ? WHERE entry_id = ?`, notes, id)
	return err
}

// Redact overwrites prompt/response/notes with the literal string
// "[redacted]" while preserving every metadata column. The FTS index
// is re-synced so the redacted entry no longer matches old keywords.
// Used when the user knows an entry contains sensitive content the
// auto-scrubber missed.
func (s *Store) Redact(ctx context.Context, id string) error {
	const marker = "[redacted]"
	if _, err := s.client.Entry.UpdateOneID(id).
		SetPrompt(marker).
		SetResponse(marker).
		SetNotes(marker).
		SetRaw("").
		Save(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE entries_fts SET prompt = ?, response = ?, notes = ? WHERE entry_id = ?`,
		marker, marker, marker, id,
	)
	return err
}

// PurgeBefore hard-deletes every entry created before t and its
// matching FTS rows. Returns the number deleted. Destructive — CLI
// should require an explicit --yes.
func (s *Store) PurgeBefore(ctx context.Context, t time.Time) (int, error) {
	ids, err := s.client.Entry.Query().
		Where(entry.CreatedAtLT(t)).
		IDs(ctx)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	n, err := s.client.Entry.Delete().Where(entry.IDIn(ids...)).Exec(ctx)
	if err != nil {
		return 0, err
	}
	// FTS5 doesn't auto-cascade. Clean up matching rows one by one;
	// volume here is bounded by the same predicate, so a single batched
	// IN clause would also work but the per-row delete keeps the SQL
	// simple and avoids a parameter-limit edge case.
	for _, id := range ids {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM entries_fts WHERE entry_id = ?`, id); err != nil {
			return n, err
		}
	}
	return n, nil
}

// Stats is a snapshot of the store for reporting. Time fields are
// pointers so JSON-encoded output omits them when the DB is empty.
type Stats struct {
	Total        int            `json:"total"`
	Starred      int            `json:"starred"`
	ByTool       map[string]int `json:"by_tool"`
	ByProject    map[string]int `json:"by_project"`
	BySession    int            `json:"distinct_sessions"`
	FirstEntryAt *time.Time     `json:"first_entry_at,omitempty"`
	LastEntryAt  *time.Time     `json:"last_entry_at,omitempty"`
}

// ComputeStats aggregates simple counts across the whole DB.
// One full scan; cheap until we hit ~100k entries.
func (s *Store) ComputeStats(ctx context.Context) (Stats, error) {
	var st Stats
	st.ByTool = map[string]int{}
	st.ByProject = map[string]int{}

	entries, err := s.All(ctx)
	if err != nil {
		return st, err
	}
	sessions := map[string]struct{}{}
	for _, e := range entries {
		st.Total++
		if e.Starred {
			st.Starred++
		}
		st.ByTool[nonEmpty(e.Tool, "(none)")]++
		st.ByProject[nonEmpty(e.Project, "(none)")]++
		if e.SessionID != "" {
			sessions[e.SessionID] = struct{}{}
		}
		t := e.CreatedAt
		if st.FirstEntryAt == nil || t.Before(*st.FirstEntryAt) {
			tt := t
			st.FirstEntryAt = &tt
		}
		if st.LastEntryAt == nil || t.After(*st.LastEntryAt) {
			tt := t
			st.LastEntryAt = &tt
		}
	}
	st.BySession = len(sessions)
	return st, nil
}

// RawHashExists reports whether an entry with the given raw blob has
// already been imported. Used by `ailog import` for idempotent
// transcript backfill — the raw column doubles as a SHA-256 dedup key.
func (s *Store) RawHashExists(ctx context.Context, rawHash string) (bool, error) {
	if rawHash == "" {
		return false, nil
	}
	var n int
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE raw = ?`, rawHash)
	if err := row.Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// nonEmpty returns s if it's non-empty, otherwise the fallback. Used
// by Stats to bucket entries with missing tool/project under "(none)".
func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
