package store

import (
	"context"
	"fmt"

	"github.com/khanakia/ai-logger/ent"
	"github.com/khanakia/ai-logger/ent/entry"
)

// SearchFilter narrows Search results. Zero-value fields mean "any".
type SearchFilter struct {
	Project   string
	Tool      string
	SessionID string
	Branch    string
	Limit     int
}

// Search runs an FTS5 query and then narrows results in ent by the
// supplied filter. The two-step (FTS first, then ent narrow) keeps us
// off-chart with FTS5 column constraints and lets us reuse ent's
// generated predicates for the structured fields.
func (s *Store) Search(ctx context.Context, query string, f SearchFilter) ([]*Entry, error) {
	ids, err := ftsSearch(ctx, s.db, query, f.Limit)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	q := s.client.Entry.Query().Where(entry.IDIn(ids...))
	if f.Project != "" {
		q = q.Where(entry.ProjectEQ(f.Project))
	}
	if f.Tool != "" {
		q = q.Where(entry.ToolEQ(f.Tool))
	}
	if f.SessionID != "" {
		q = q.Where(entry.SessionIDEQ(f.SessionID))
	}
	if f.Branch != "" {
		q = q.Where(entry.GitBranchEQ(f.Branch))
	}
	return q.Order(ent.Desc(entry.FieldCreatedAt)).All(ctx)
}

// GetByID returns an entry by exact id. CLI prefix resolution should
// run through ResolveIDPrefix first.
func (s *Store) GetByID(ctx context.Context, id string) (*Entry, error) {
	return s.client.Entry.Get(ctx, id)
}

// ResolveIDPrefix expands a short id prefix (e.g. "019da607-dfac") to a
// full UUID. Errors on ambiguous or missing matches. A 36-char input
// is treated as a full UUID and passes through unchanged.
//
// Implementation note: ent doesn't generate IDHasPrefix for string ids,
// so we drop to raw SQL. The LIMIT 2 lets us detect ambiguity cheaply
// without scanning the whole table.
func (s *Store) ResolveIDPrefix(ctx context.Context, prefix string) (string, error) {
	if len(prefix) == 36 {
		return prefix, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM entries WHERE id LIKE ? || '%' LIMIT 2`,
		prefix,
	)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var found []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		found = append(found, id)
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("no entry with id prefix %q", prefix)
	case 1:
		return found[0], nil
	default:
		return "", fmt.Errorf("ambiguous id prefix %q (matches multiple)", prefix)
	}
}

// Recent returns the N most recent entries by created_at. limit <= 0
// defaults to 10.
func (s *Store) Recent(ctx context.Context, limit int) ([]*Entry, error) {
	if limit <= 0 {
		limit = 10
	}
	return s.client.Entry.Query().
		Order(ent.Desc(entry.FieldCreatedAt)).
		Limit(limit).
		All(ctx)
}

// SessionEntries returns every entry in a session, ordered by
// turn_index ascending (with created_at as a tiebreaker for legacy
// rows that didn't compute a turn index).
func (s *Store) SessionEntries(ctx context.Context, sessionID string) ([]*Entry, error) {
	return s.client.Entry.Query().
		Where(entry.SessionIDEQ(sessionID)).
		Order(ent.Asc(entry.FieldTurnIndex), ent.Asc(entry.FieldCreatedAt)).
		All(ctx)
}

// All returns every entry in the DB, newest first. Use with care on
// large stores; primary use is reporting (stats, export, templates).
func (s *Store) All(ctx context.Context) ([]*Entry, error) {
	return s.client.Entry.Query().
		Order(ent.Desc(entry.FieldCreatedAt)).
		All(ctx)
}
