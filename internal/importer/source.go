// Package importer is `ailog import`'s backfill driver. It reads every
// transcript of an agent's homes through agentio (internal/agents) and
// writes each turn into the store with ailog's own policy: redaction,
// dedup, pairing a prompt with its answer, project resolution and the
// drift watchdog. It knows no vendor transcript format.
//
// Idempotency, in the order the driver checks it (the authoritative
// mechanism is the dedup table, the watermark only saves work):
//
//  1. import_state — per-transcript mtime+size watermark and read cursor.
//     An unchanged file is skipped; a grown one is read from the cursor
//     the last read returned. --force reads every file from the start.
//  2. import_lines — per-message dedup keys: the SHA-256 of the record's
//     bytes (agentio Turn.Raw; the key every older ailog wrote) and the
//     vendor message id (agentio Turn.ID). A hit on either skips the turn
//     and records the other key for the same entry.
//  3. A last-resort match on the stored rows (same tool, session,
//     transcript time and text; for an answer, the latest prompt of the
//     session already holding that text), so a turn stored under a key
//     the current reader no longer produces is not written twice.
package importer

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/ubgo/agentio"

	"github.com/khanakia/ai-logger/internal/agents"
)

// Role distinguishes the user's prompt side of a turn from the
// assistant's response side.
type Role string

// Role values.
const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// msgKeyPrefix starts a message-id dedup key in import_lines.
const msgKeyPrefix = "msg:"

// Record is one normalized message ready to store: an agentio.Turn plus
// the transcript it came from and the vendor annotation.
type Record struct {
	Tool        string    // stored tool name, e.g. "claude-code"
	ToolVersion string    // agent version that wrote the record
	SessionID   string    // the agent's conversation id
	SessionName string    // conversation title (agents with UseTitle)
	CWD         string    // working directory the agent recorded
	Role        Role      // user | assistant
	Text        string    // message text
	Model       string    // assistant model id
	StopReason  string    // assistant stop reason
	Timestamp   time.Time // transcript timestamp (zero = unknown)
	// LineHash is the SHA-256 hex of the record's bytes (agentio Turn.Raw:
	// a JSONL line without its terminator, the bytes older ailog hashed
	// from bufio.ScanLines; an opencode message file as stored): the dedup
	// key every row imported so far carries.
	LineHash string
	// MsgKey is the message-id dedup key; "" when the format has no id.
	MsgKey string
	// Source is the transcript's import key (path or database session).
	Source string

	// Token usage; reasoning tokens are folded into TokensOut, providers
	// bill them as output.
	TokensIn         int
	TokensOut        int
	TokensCacheRead  int
	TokensCacheWrite int

	// PermissionMode, Terminal, TerminalTitle and Extras come from the
	// agent's Annotation (agents.Annotation).
	PermissionMode string
	Terminal       string
	TerminalTitle  string
	Extras         map[string]any
}

// recordOf maps one agentio turn of transcript t to a Record. ok=false for
// turns ailog does not store: interim progress messages (Codex
// commentary) and turns without text (a tool-only assistant step).
func recordOf(a agents.Agent, t agents.Transcript, turn agentio.Turn, ann agents.Annotation) (Record, bool) {
	if turn.Interim || turn.Text == "" {
		return Record{}, false
	}
	var role Role
	switch turn.Role {
	case agentio.RoleUser:
		role = RoleUser
	case agentio.RoleAssistant:
		role = RoleAssistant
	default:
		return Record{}, false
	}
	r := Record{
		Tool:           a.Tool,
		ToolVersion:    turn.AgentVersion,
		SessionID:      firstNonEmpty(turn.ConversationID, t.ConversationID),
		CWD:            firstNonEmpty(turn.Dir, t.Dir),
		Role:           role,
		Text:           turn.Text,
		Timestamp:      turn.Time,
		Source:         t.Key,
		PermissionMode: ann.PermissionMode,
		Terminal:       ann.Terminal,
		TerminalTitle:  ann.TerminalTitle,
		Extras:         ann.Extras,
	}
	if a.UseTitle {
		r.SessionName = t.Title
	}
	if len(turn.Raw) > 0 {
		r.LineHash = hashBytes(turn.Raw)
	}
	if turn.ID != "" {
		r.MsgKey = msgKeyPrefix + a.Tool + ":" + r.SessionID + ":" + turn.ID
	}
	if role == RoleAssistant {
		r.Model = firstNonEmpty(ann.Model, turn.Model)
		r.StopReason = turn.StopReason
		if tk := turn.Tokens; tk != nil {
			r.TokensIn = int(tk.Input)
			r.TokensOut = int(tk.Output + tk.Reasoning)
			r.TokensCacheRead = int(tk.CachedInput)
			r.TokensCacheWrite = int(tk.CacheWrite)
		}
	}
	return r, true
}

// anchor is the drift watchdog's "this answer looks healthy" check: an
// assistant record carrying input tokens. Every supported agent writes
// usage with its answers, so a transcript whose answers all lack it most
// likely means the vendor renamed the usage fields.
func anchor(r Record) bool { return r.Role == RoleAssistant && r.TokensIn > 0 }

// hashBytes is the SHA-256 hex of b.
func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// newerThan reports whether version v is newer than tested (agentio
// version order); false when either does not parse.
func newerThan(v, tested string) bool {
	a, ok := agentio.ParseVersion(v)
	if !ok {
		return false
	}
	b, ok := agentio.ParseVersion(tested)
	if !ok {
		return false
	}
	return a.Compare(b) > 0
}
