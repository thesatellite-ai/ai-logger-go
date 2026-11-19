// Package agents binds ailog's stored tool names to the agentio agents
// that know each coding agent's homes, transcripts and hooks
// (github.com/ubgo/agentio). Every vendor fact ailog needs comes through
// here: which homes exist, which transcripts a home holds, how to read a
// transcript's turns from a cursor, and how to install or decode a hook.
// ailog itself keeps only its own policy (storage, redaction, dedup,
// pairing, environment capture).
//
// The tool names stored in entries.tool predate agentio and stay as they
// are ("claude-code", not agentio's "claude"); New maps between the two.
package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ubgo/agentio"
	"github.com/ubgo/agentio/builtin"
)

// Stored tool names (entries.tool). Never rename one: rows, URLs and the
// hook command lines in users' settings files carry them.
const (
	ToolClaude   = "claude-code"
	ToolCodex    = "codex"
	ToolGemini   = "gemini"
	ToolPi       = "pi"
	ToolOpencode = "opencode"
)

// ToolNames lists every stored tool name ailog captures, in registry
// order. Pure.
func ToolNames() []string {
	return []string{ToolClaude, ToolCodex, ToolGemini, ToolPi, ToolOpencode}
}

// ClaudeHomeEnv narrows ailog to one Claude Code home (a CLAUDE_CONFIG_DIR
// style folder) for import, hooks and skill install. Unset means every
// Claude home agentio finds (~/.claude plus ~/.claude-*).
const ClaudeHomeEnv = "AILOG_CLAUDE_HOME"

// Annotation is what ailog stores for a turn beyond the neutral
// agentio.Turn fields: values a vendor records per session or per turn
// that the neutral model has no field for yet (agentio gap A8, neutral
// turn context). Zero fields are "unknown".
type Annotation struct {
	// PermissionMode is Codex's approval policy (same intent as Claude's
	// permission mode).
	PermissionMode string
	// Terminal and TerminalTitle are Codex's originator (Codex Desktop,
	// Codex CLI) and session source (vscode, cli): they describe the same
	// idea as the live-capture environment columns.
	Terminal, TerminalTitle string
	// Model replaces Turn.Model when set (opencode keeps the bare model id
	// in its rows, Turn.Model is "provider/model").
	Model string
	// Extras are small vendor keys ("codex.sandbox_type", "opencode.cost_usd")
	// encoded into the entry's raw column.
	Extras map[string]any
}

// Annotator maps one turn of a transcript to its Annotation.
type Annotator func(agentio.Turn) Annotation

// Transcript is one transcript of a home: a file (Path set) or a session
// held in a database (Path "", Key names it).
type Transcript struct {
	// Key identifies the transcript in ailog's import_state and
	// import_lines tables: the file path, or "<database path>#<session id>".
	Key string
	// Path is the transcript file; "" for a database-held session.
	Path           string
	ConversationID string
	Title, Dir     string
	Home           agentio.Home
	Updated        time.Time

	read     func(from agentio.Offset, fn func(agentio.Turn) error) (agentio.Offset, error)
	annotate func() Annotator
}

// Turns reads the transcript's turns from cursor from (0 = the start) and
// returns the cursor to store for the next read (agentio's opaque Offset).
// Disk · Read.
func (t Transcript) Turns(from agentio.Offset, fn func(agentio.Turn) error) (agentio.Offset, error) {
	return t.read(from, fn)
}

// Annotator prepares the per-turn annotation of this transcript (it may
// read the file once, Disk · Read). A transcript without vendor extras
// returns an annotator that reports nothing.
func (t Transcript) Annotator() Annotator {
	if t.annotate == nil {
		return func(agentio.Turn) Annotation { return Annotation{} }
	}
	return t.annotate()
}

// Agent is one coding agent as ailog uses it.
type Agent struct {
	// Tool is the stored tool name.
	Tool string
	// Neutral is the agentio view; capabilities through agentio's
	// accessors (agentio.HooksOf, agentio.TurnFilesOf, ...).
	Neutral agentio.Agent
	// TestedMax is the newest vendor version agentio's reader was checked
	// against; the importer's drift watchdog warns past it.
	TestedMax string
	// UseTitle stores the conversation title as the entry's session name.
	// Off for claude-code and codex, which never stored one (their rows
	// stay as they were).
	UseTitle bool
	// WholeRecordKey makes the importer's legacy dedup key the hash of
	// Turn.Raw exactly as read (opencode: one JSON file per message, the
	// key older ailog wrote). Unset, the key is the hash of the record's
	// line without its line terminator, the key older ailog wrote for
	// JSONL transcripts (bufio.ScanLines bytes).
	WholeRecordKey bool

	homeAt      func(dir string) (agentio.Home, error)
	homes       func() ([]agentio.Home, error)
	transcripts func(ctx context.Context, h agentio.Home) ([]Transcript, error)
}

// HomeAt returns the agent's home at dir, without checking it holds
// anything. Pure, except Codex which resolves the folder (Disk · Read).
func (a Agent) HomeAt(dir string) (agentio.Home, error) { return a.homeAt(dir) }

// Homes lists the homes on disk (default first). Disk · Read.
func (a Agent) Homes() ([]agentio.Home, error) {
	if a.homes != nil {
		return a.homes()
	}
	return a.Neutral.Homes()
}

// Transcripts lists every transcript of h, oldest update first, so turns
// of a conversation split over files are read in order. Disk · Read.
func (a Agent) Transcripts(ctx context.Context, h agentio.Home) ([]Transcript, error) {
	ts, err := a.transcripts(ctx, h)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(ts, func(i, j int) bool { return ts[i].Updated.Before(ts[j].Updated) })
	return ts, nil
}

// Hooks is the agent's hook installer and decoder; false for agents with
// no shell-command hooks (pi, opencode).
func (a Agent) Hooks() (agentio.HookInstaller, bool) { return agentio.HooksOf(a.Neutral) }

// Owner maps a transcript path a hook payload names to its home and
// conversation (agentio TurnFilesOf.TranscriptOwner). Disk · Read.
func (a Agent) Owner(path string) (agentio.TranscriptRef, error) {
	r, ok := agentio.TurnFilesOf(a.Neutral)
	if !ok {
		return agentio.TranscriptRef{}, fmt.Errorf("%s: %w", a.Tool, errNoTurnFiles)
	}
	return r.TranscriptOwner(path)
}

// TurnsAt reads the turns of the transcript at path from cursor from.
// Disk · Read.
func (a Agent) TurnsAt(path string, from agentio.Offset, fn func(agentio.Turn) error) (agentio.Offset, error) {
	r, ok := agentio.TurnFilesOf(a.Neutral)
	if !ok {
		return from, fmt.Errorf("%s: %w", a.Tool, errNoTurnFiles)
	}
	return r.TurnsAt(path, from, fn)
}

// errNoTurnFiles means the agent has no path-based transcript reader.
var errNoTurnFiles = errors.New("agent cannot read a transcript by path")

// Registry is every agent ailog captures, in a fixed order.
type Registry struct {
	all []Agent
}

// All returns every agent, in registry order (claude-code, codex, gemini,
// pi, opencode).
func (r Registry) All() []Agent { return append([]Agent(nil), r.all...) }

// ByTool finds an agent by stored tool name.
func (r Registry) ByTool(tool string) (Agent, bool) {
	for _, a := range r.all {
		if a.Tool == tool {
			return a, true
		}
	}
	return Agent{}, false
}

// Tools lists the stored tool names, in registry order.
func (r Registry) Tools() []string {
	out := make([]string, len(r.all))
	for i, a := range r.all {
		out[i] = a.Tool
	}
	return out
}

// WithHooks lists the agents that can run a command hook.
func (r Registry) WithHooks() []Agent {
	var out []Agent
	for _, a := range r.all {
		if _, ok := a.Hooks(); ok {
			out = append(out, a)
		}
	}
	return out
}

// New builds the registry over host. claudeHome, when non-empty, is the
// only Claude home ailog uses (AILOG_CLAUDE_HOME).
func New(host agentio.Host, claudeHome string) (Registry, error) {
	b, err := builtin.New(host)
	if err != nil {
		return Registry{}, fmt.Errorf("agents: %w", err)
	}
	return Registry{all: []Agent{
		claudeAgent(b.Claude, claudeHome),
		codexAgent(b.Codex),
		geminiAgent(b.Gemini),
		piAgent(b.Pi),
		opencodeAgent(b.Opencode),
	}}, nil
}

// FromOS builds the registry over the real machine (agentio.HostFromOS),
// honouring AILOG_CLAUDE_HOME.
func FromOS() (Registry, error) {
	host, err := agentio.HostFromOS()
	if err != nil {
		return Registry{}, fmt.Errorf("agents: %w", err)
	}
	return New(host, strings.TrimSpace(os.Getenv(ClaudeHomeEnv)))
}

// conversationTranscripts lists the file transcripts of h through the
// neutral ConversationLister, one per conversation with a Path.
// Disk · Read.
func conversationTranscripts(ctx context.Context, a agentio.Agent, h agentio.Home, read func(path string, from agentio.Offset, fn func(agentio.Turn) error) (agentio.Offset, error)) ([]agentio.Conversation, []Transcript, error) {
	l, ok := agentio.ConversationsOf(a)
	if !ok {
		return nil, nil, nil
	}
	cs, err := l.Conversations(ctx, h, agentio.ConversationQuery{})
	if err != nil {
		return nil, nil, err
	}
	out := make([]Transcript, 0, len(cs))
	for _, c := range cs {
		if c.Path == "" {
			continue
		}
		out = append(out, fileTranscript(c.Path, c, read))
	}
	return cs, out, nil
}

// fileTranscript is a Transcript over one file of conversation c.
func fileTranscript(path string, c agentio.Conversation, read func(path string, from agentio.Offset, fn func(agentio.Turn) error) (agentio.Offset, error)) Transcript {
	return Transcript{
		Key: path, Path: path, ConversationID: c.ID, Title: c.Title, Dir: c.Dir, Home: c.Home, Updated: c.Updated,
		read: func(from agentio.Offset, fn func(agentio.Turn) error) (agentio.Offset, error) {
			return read(path, from, fn)
		},
	}
}

// neutralReader is the TurnFileReader read of a neutral agent.
func neutralReader(a agentio.Agent) func(path string, from agentio.Offset, fn func(agentio.Turn) error) (agentio.Offset, error) {
	return func(path string, from agentio.Offset, fn func(agentio.Turn) error) (agentio.Offset, error) {
		r, ok := agentio.TurnFilesOf(a)
		if !ok {
			return from, errNoTurnFiles
		}
		return r.TurnsAt(path, from, fn)
	}
}
