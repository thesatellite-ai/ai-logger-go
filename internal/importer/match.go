package importer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/khanakia/ai-logger/internal/redact"
	"github.com/khanakia/ai-logger/internal/store"
)

// claimWindow is how far apart a live-hook row's capture time and the
// transcript time of the same prompt may be for the importer to treat
// them as one turn. Measured on a real log.db (2026-11-19, 9,859 hook
// rows whose prompt an import duplicated): median 0.09 s, 99th
// percentile 19 s, largest 212 s (a prompt queued while the agent was
// busy); none between 212 s and 1 h. Ten minutes covers every one with a
// wide margin, and a same-text prompt sent again in the same session
// minutes later still has its own hook row nearer in time.
const claimWindow = 10 * time.Minute

// runState is what one Run remembers across transcripts: the session rows
// read so far (a session is loaded once, then kept in step with every
// write) and the prompt each session's next answer attaches to.
type runState struct {
	st       *store.Store
	sessions map[string]*sessionState
	// pending is the prompt entry the next answer of a session attaches
	// to, mirroring the live hooks' pairing.
	pending map[string]string
}

// sessionState is one session's rows, ordered by transcript time.
type sessionState struct {
	rows []store.SessionRow
}

func newRunState(st *store.Store) *runState {
	return &runState{st: st, sessions: map[string]*sessionState{}, pending: map[string]string{}}
}

// session loads (once) the rows of a tool's session.
func (rs *runState) session(ctx context.Context, tool, sid string) (*sessionState, error) {
	k := tool + "\x00" + sid
	if s, ok := rs.sessions[k]; ok {
		return s, nil
	}
	rows, err := rs.st.SessionRows(ctx, tool, sid)
	if err != nil {
		return nil, fmt.Errorf("session %s: %w", sid, err)
	}
	s := &sessionState{rows: rows}
	rs.sessions[k] = s
	return s, nil
}

// add records a row written by this run, keeping time order.
func (s *sessionState) add(r store.SessionRow) {
	i := len(s.rows)
	for i > 0 && s.rows[i-1].At().After(r.At()) {
		i--
	}
	s.rows = append(s.rows, store.SessionRow{})
	copy(s.rows[i+1:], s.rows[i:])
	s.rows[i] = r
}

// byID finds a row.
func (s *sessionState) byID(id string) *store.SessionRow {
	for i := range s.rows {
		if s.rows[i].ID == id {
			return &s.rows[i]
		}
	}
	return nil
}

// promptMatch finds the stored row of a prompt read from a transcript at
// time at: a row with the same text (whitespace-trimmed) stamped at the
// same transcript instant (an earlier import), or, failing that, the
// nearest live-hook row (no import key yet) captured within claimWindow.
// hook reports the second kind. nil when none.
func (s *sessionState) promptMatch(text string, at time.Time) (row *store.SessionRow, hook bool) {
	if at.IsZero() {
		return nil, false
	}
	want := strings.TrimSpace(text)
	var best *store.SessionRow
	var bestGap time.Duration
	for i := range s.rows {
		r := &s.rows[i]
		if strings.TrimSpace(r.Prompt) != want {
			continue
		}
		if r.At().Equal(at) {
			return r, false // stored by an earlier import (keys lost or never written)
		}
		if r.Linked {
			continue
		}
		gap := r.At().Sub(at)
		if gap < 0 {
			gap = -gap
		}
		if gap <= claimWindow && (best == nil || gap < bestGap) {
			best, bestGap = r, gap
		}
	}
	return best, best != nil
}

// turnOf returns the latest prompt row at or before at (any time when at
// is zero) and the answer rows written after it up to at: the stored rows
// of the turn an answer read at time at belongs to.
func (s *sessionState) turnOf(at time.Time) (prompt *store.SessionRow, rows []*store.SessionRow) {
	start := -1
	for i := range s.rows {
		if !at.IsZero() && s.rows[i].At().After(at) {
			break
		}
		if s.rows[i].Prompt != "" {
			start = i
		}
	}
	if start < 0 {
		return nil, nil
	}
	for i := start; i < len(s.rows); i++ {
		if !at.IsZero() && s.rows[i].At().After(at) {
			break
		}
		if i > start && s.rows[i].Prompt != "" {
			break
		}
		rows = append(rows, &s.rows[i])
	}
	return &s.rows[start], rows
}

// storeRecord writes one record unless it is already stored, and records
// its dedup keys. written=true when a row was inserted or changed.
func storeRecord(ctx context.Context, rs *runState, r Record, opts *Options, stats *Stats) (written bool, err error) {
	st := rs.st
	text := redact.Scrub(r.Text)

	// 1. Dedup keys: the record's bytes, then its message id.
	id, ok, err := st.ImportLineEntry(ctx, r.LineHash)
	if err != nil {
		return false, fmt.Errorf("dedup lookup: %w", err)
	}
	if ok {
		recordKeys(ctx, st, r, id)
		stats.RecordsSkipped++
		return false, nil
	}
	if id, ok, err = st.ImportLineEntry(ctx, r.MsgKey); err != nil {
		return false, fmt.Errorf("dedup lookup: %w", err)
	} else if ok {
		recordKeys(ctx, st, r, id)
		if r.Role == RoleAssistant && r.LineHash != "" {
			// Known message, new bytes: the agent rewrote it (Gemini
			// re-appends an answer once its token count is known; a
			// database row is updated in place). The newer copy
			// replaces the stored answer and its usage.
			if err := attachAssistant(ctx, st, id, r, text); err != nil {
				return false, err
			}
			if s, err := rs.session(ctx, r.Tool, r.SessionID); err == nil {
				if row := s.byID(id); row != nil {
					row.Response = text
				}
			}
			stats.Updated++
			return true, nil
		}
		stats.RecordsSkipped++
		return false, nil
	}

	sess, err := rs.session(ctx, r.Tool, r.SessionID)
	if err != nil {
		return false, err
	}
	switch r.Role {
	case RoleUser:
		return storePrompt(ctx, rs, sess, r, text, opts, stats)
	case RoleAssistant:
		return storeAnswer(ctx, rs, sess, r, text, opts, stats)
	}
	return false, nil
}

// storePrompt links a prompt to its stored row (an earlier import, or the
// row the live hook wrote for the same turn) or inserts it.
func storePrompt(ctx context.Context, rs *runState, sess *sessionState, r Record, text string, opts *Options, stats *Stats) (bool, error) {
	st := rs.st
	if row, hook := sess.promptMatch(text, r.Timestamp); row != nil {
		recordKeys(ctx, st, r, row.ID)
		row.Linked = true
		stats.RecordsSkipped++
		if !hook {
			stats.RecordsMatched++
			return false, nil
		}
		// The live hook's row of this turn: it keeps every value it
		// captured and gains the transcript's where it has none.
		ts := r.Timestamp
		changed, err := st.FillEmpty(ctx, row.ID, store.FillInput{
			ToolVersion: r.ToolVersion, PermissionMode: r.PermissionMode, Terminal: r.Terminal,
			TerminalTitle: r.TerminalTitle, SessionName: r.SessionName, TranscriptAt: &ts,
		})
		if err != nil {
			return false, err
		}
		row.TranscriptAt = &ts
		rs.pending[r.SessionID] = row.ID
		stats.Linked++
		return changed, nil
	}
	id, err := writeUserEntry(ctx, st, r, text, opts)
	if err != nil {
		return false, err
	}
	stats.Inserted++
	rs.pending[r.SessionID] = id
	recordKeys(ctx, st, r, id)
	sess.add(store.SessionRow{ID: id, Prompt: text, CreatedAt: r.Timestamp, TranscriptAt: timePtr(r.Timestamp), Linked: true})
	return true, nil
}

// storeAnswer links an answer already stored in its turn, attaches it to
// the turn's prompt when that has no answer yet, or writes it as its own
// row.
func storeAnswer(ctx context.Context, rs *runState, sess *sessionState, r Record, text string, opts *Options, stats *Stats) (bool, error) {
	st := rs.st
	meta := store.FillInput{
		ToolVersion: r.ToolVersion, PermissionMode: r.PermissionMode, Model: r.Model, StopReason: r.StopReason,
		TokensIn: r.TokensIn, TokensOut: r.TokensOut, TokensCacheRead: r.TokensCacheRead, TokensCacheWrite: r.TokensCacheWrite,
	}
	prompt, turn := sess.turnOf(r.Timestamp)
	want := strings.TrimSpace(text)
	for _, row := range turn {
		if strings.TrimSpace(row.Response) == want {
			// Already stored in this turn (the live Stop hook's answer, or
			// an earlier import): link it, fill what the hook lacked.
			recordKeys(ctx, st, r, row.ID)
			stats.RecordsSkipped++
			stats.RecordsMatched++
			changed, err := st.FillEmpty(ctx, row.ID, meta)
			return changed, err
		}
	}
	target := rs.pending[r.SessionID]
	if target == "" && prompt != nil {
		target = prompt.ID
	}
	if row := sess.byID(target); row != nil && row.Response == "" {
		// The turn's prompt has no answer yet: this is it. Fill, never
		// overwrite, so a live-hook row keeps what it captured.
		fill := meta
		fill.Response = text
		if _, err := st.FillEmpty(ctx, target, fill); err != nil {
			return false, err
		}
		row.Response = text
		stats.Attached++
		delete(rs.pending, r.SessionID)
		recordKeys(ctx, st, r, target)
		return true, nil
	}
	// Standalone answer — its own row so the response isn't lost.
	id, err := writeStandaloneAssistant(ctx, st, r, text, opts)
	if err != nil {
		return false, err
	}
	stats.Standalone++
	recordKeys(ctx, st, r, id)
	sess.add(store.SessionRow{ID: id, Response: text, CreatedAt: r.Timestamp, TranscriptAt: timePtr(r.Timestamp), Linked: true})
	return true, nil
}

// timePtr is &t, or nil for the zero time.
func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
