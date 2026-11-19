package store

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

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
//
// When query is empty, the FTS5 step is skipped and only the structured
// filters apply — useful for "show me everything in project X" links
// from the projects / sessions pages.
func (s *Store) Search(ctx context.Context, query string, f SearchFilter) ([]*Entry, error) {
	q := s.client.Entry.Query()
	if query != "" {
		ids, err := ftsSearch(ctx, s.db, query, f.Limit)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return nil, nil
		}
		q = q.Where(entry.IDIn(ids...))
	}
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
	q = q.Order(ent.Desc(entry.FieldCreatedAt))
	if f.Limit > 0 {
		q = q.Limit(f.Limit)
	}
	return q.All(ctx)
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
	defer func() { _ = rows.Close() }()
	var found []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		found = append(found, id)
	}
	if err := rows.Err(); err != nil {
		return "", err
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

// SessionRow is the part of an entry the importer matches on.
type SessionRow struct {
	ID, Prompt, Response string
	CreatedAt            time.Time
	// TranscriptAt is the transcript time when known (imported or
	// matched rows); nil for a row only a live hook wrote.
	TranscriptAt *time.Time
	TurnIndex    int
	// Linked reports that an import key (import_lines) points at the row:
	// the importer stored it, or already matched it to a transcript turn.
	Linked bool
}

// SessionRows lists a tool's entries of one session, oldest first, with
// their import-link state. Times come back as instants (rows written in
// different zones compare correctly in Go, not as SQL text).
func (s *Store) SessionRows(ctx context.Context, tool, sessionID string) ([]SessionRow, error) {
	if sessionID == "" {
		return nil, nil
	}
	es, err := s.client.Entry.Query().
		Where(entry.SessionIDEQ(sessionID), entry.ToolEQ(tool)).
		Select(entry.FieldID, entry.FieldPrompt, entry.FieldResponse, entry.FieldCreatedAt, entry.FieldTranscriptAt, entry.FieldTurnIndex).
		All(ctx)
	if err != nil {
		return nil, err
	}
	linked, err := s.linkedIDs(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	out := make([]SessionRow, len(es))
	for i, e := range es {
		out[i] = SessionRow{ID: e.ID, Prompt: e.Prompt, Response: e.Response, CreatedAt: e.CreatedAt, TranscriptAt: e.TranscriptAt,
			TurnIndex: e.TurnIndex, Linked: linked[e.ID]}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At().Before(out[j].At()) })
	return out, nil
}

// At is when the turn happened by the transcript's clock: TranscriptAt
// when known, else the capture time. Pure.
func (r SessionRow) At() time.Time {
	if r.TranscriptAt != nil {
		return *r.TranscriptAt
	}
	return r.CreatedAt
}

// linkedIDs is the set of a session's entry ids that an import key
// points at.
func (s *Store) linkedIDs(ctx context.Context, sessionID string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT l.entry_id FROM import_lines l JOIN entries e ON e.id = l.entry_id
		WHERE e.session_id = ?`, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// FillInput is what FillEmpty may write; zero fields are ignored.
type FillInput struct {
	ToolVersion, PermissionMode, Terminal, TerminalTitle, SessionName string
	Response, Model, StopReason                                       string
	TokensIn, TokensOut, TokensCacheRead, TokensCacheWrite            int
	TranscriptAt                                                      *time.Time
}

// FillEmpty sets each non-zero field of in on entry id only where the
// entry's own value is empty (zero), so values a live hook captured are
// never overwritten. Returns whether anything changed.
func (s *Store) FillEmpty(ctx context.Context, id string, in FillInput) (bool, error) {
	e, err := s.client.Entry.Get(ctx, id)
	if err != nil {
		return false, err
	}
	u := s.client.Entry.UpdateOneID(id)
	changed := false
	setS := func(have, want string, set func(string)) {
		if have == "" && want != "" {
			set(want)
			changed = true
		}
	}
	setI := func(have, want int, set func(int)) {
		if have == 0 && want != 0 {
			set(want)
			changed = true
		}
	}
	setS(e.ToolVersion, in.ToolVersion, func(v string) { u.SetToolVersion(v) })
	setS(e.PermissionMode, in.PermissionMode, func(v string) { u.SetPermissionMode(v) })
	setS(e.Terminal, in.Terminal, func(v string) { u.SetTerminal(v) })
	setS(e.TerminalTitle, in.TerminalTitle, func(v string) { u.SetTerminalTitle(v) })
	setS(e.SessionName, in.SessionName, func(v string) { u.SetSessionName(v) })
	setS(e.Model, in.Model, func(v string) { u.SetModel(v) })
	setS(e.StopReason, in.StopReason, func(v string) { u.SetStopReason(v) })
	setI(e.TokenCountIn, in.TokensIn, func(v int) { u.SetTokenCountIn(v) })
	setI(e.TokenCountOut, in.TokensOut, func(v int) { u.SetTokenCountOut(v) })
	setI(e.TokenCountCacheRead, in.TokensCacheRead, func(v int) { u.SetTokenCountCacheRead(v) })
	setI(e.TokenCountCacheCreate, in.TokensCacheWrite, func(v int) { u.SetTokenCountCacheCreate(v) })
	if e.TranscriptAt == nil && in.TranscriptAt != nil {
		u.SetTranscriptAt(*in.TranscriptAt)
		changed = true
	}
	fillResponse := e.Response == "" && in.Response != ""
	if fillResponse {
		u.SetResponse(in.Response)
		changed = true
	}
	if !changed {
		return false, nil
	}
	if _, err := u.Save(ctx); err != nil {
		return false, fmt.Errorf("fill entry: %w", err)
	}
	if fillResponse {
		if err := ftsUpdateResponse(ctx, s.db, id, in.Response); err != nil {
			return true, err
		}
	}
	return true, nil
}

// EntryFill returns the fields of entry id a merge copies into another
// row (FillEmpty input), with TranscriptAt set to the entry's transcript
// time (its created_at when it has none: imported rows are stamped with
// the transcript time).
func (s *Store) EntryFill(ctx context.Context, id string) (FillInput, error) {
	e, err := s.client.Entry.Get(ctx, id)
	if err != nil {
		return FillInput{}, err
	}
	at := e.CreatedAt
	if e.TranscriptAt != nil {
		at = *e.TranscriptAt
	}
	return FillInput{
		ToolVersion: e.ToolVersion, PermissionMode: e.PermissionMode, Terminal: e.Terminal, TerminalTitle: e.TerminalTitle,
		SessionName: e.SessionName, Response: e.Response, Model: e.Model, StopReason: e.StopReason,
		TokensIn: e.TokenCountIn, TokensOut: e.TokenCountOut, TokensCacheRead: e.TokenCountCacheRead, TokensCacheWrite: e.TokenCountCacheCreate,
		TranscriptAt: &at,
	}, nil
}

// MergeInto folds entry from into entry into: into keeps every value it
// has and gains from's values where it has none (FillEmpty), from's
// import keys and children (parent_entry_id) move to into, and from is
// deleted with its search row. For merging an imported duplicate into the
// live-hook row of the same turn (ailog dedupe).
func (s *Store) MergeInto(ctx context.Context, from, into string) error {
	if from == into {
		return fmt.Errorf("merge: %s into itself", from)
	}
	fill, err := s.EntryFill(ctx, from)
	if err != nil {
		return err
	}
	if _, err := s.FillEmpty(ctx, into, fill); err != nil {
		return err
	}
	for _, q := range []string{
		`UPDATE import_lines SET entry_id = ? WHERE entry_id = ?`,
		`UPDATE entries SET parent_entry_id = ? WHERE parent_entry_id = ?`,
	} {
		if _, err := s.db.ExecContext(ctx, q, into, from); err != nil {
			return fmt.Errorf("merge: %w", err)
		}
	}
	if err := s.client.Entry.DeleteOneID(from).Exec(ctx); err != nil {
		return fmt.Errorf("merge: delete %s: %w", from, err)
	}
	if err := ftsDelete(ctx, s.db, from); err != nil {
		return fmt.Errorf("merge: search row: %w", err)
	}
	return nil
}

// DuplicateSessions lists the (tool, session) pairs holding the same
// non-empty prompt more than once: where ailog dedupe looks.
func (s *Store) DuplicateSessions(ctx context.Context) ([][2]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT tool, session_id FROM (
			SELECT tool, session_id FROM entries
			WHERE prompt != '' AND session_id != ''
			GROUP BY tool, session_id, trim(prompt) HAVING COUNT(*) > 1)`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out [][2]string
	for rows.Next() {
		var t, sid string
		if err := rows.Scan(&t, &sid); err != nil {
			return nil, err
		}
		out = append(out, [2]string{t, sid})
	}
	return out, rows.Err()
}

// BackupTo writes a consistent copy of the database to path (VACUUM
// INTO), refusing to overwrite an existing file.
func (s *Store) BackupTo(ctx context.Context, path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("backup: %s already exists", path)
	}
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, path); err != nil {
		return fmt.Errorf("backup to %s: %w", path, err)
	}
	return nil
}
