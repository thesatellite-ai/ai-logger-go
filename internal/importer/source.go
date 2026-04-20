// Package importer is the home for `ailog import`'s tool-agnostic
// backfill machinery. Each upstream tool (claude-code, codex, opencode,
// …) registers a Source — a four-method contract that turns a
// directory of native transcripts into a stream of normalized Records
// the store can consume.
//
// The driver in import.go walks each Source's Discover output, runs
// Parse per file, normalizes each Record into a store.InsertEntryInput
// (or AttachResponseInput for the assistant side of a turn), and uses
// the store's import_lines table for per-line idempotency. import_state
// gives the per-file mtime watermark so re-runs over a huge transcript
// tree are cheap.
//
// Adding a tool ⇒ add one file with a Source implementation and call
// Register() in init().
package importer

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Role distinguishes the user's prompt side of a turn from the
// assistant's response side. Sources MUST emit one Record per side so
// the importer can wire up turn-pairing the same way the live hooks do.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Record is one normalized message extracted from a transcript file.
// Sources fill in only what's actually present in their wire format;
// downstream code treats zero values as "unknown".
//
// LineHash MUST uniquely identify the source line (a SHA-256 of the
// raw bytes is what every concrete Source uses). It's the dedup key
// stored in import_lines.
type Record struct {
	Tool        string    // upstream tool name, e.g. "claude-code"
	ToolVersion string    // when present in the transcript line
	SessionID   string    // tool's native session id (uuid for claude-code)
	SessionName string    // human-friendly title (opencode emits this; others rarely)
	CWD         string    // working directory captured by the tool
	Role        Role      // user | assistant
	Text        string    // flattened message text
	Model       string    // assistant model id
	StopReason  string    // assistant stop reason
	Timestamp   time.Time // original transcript timestamp (zero ⇒ unknown)
	LineHash    string    // SHA-256 of raw line bytes (idempotency key)
	SourceFile  string    // absolute path to the transcript that produced this

	// Token usage — populated where the tool emits it (mostly Anthropic).
	TokensIn         int
	TokensOut        int
	TokensCacheRead  int
	TokensCacheWrite int

	// PermissionMode — claude-code: default|acceptEdits|bypassPermissions|plan
	// codex: approval_policy (on-request|never|unless-trusted) — same intent.
	PermissionMode string

	// Terminal / TerminalTitle — Codex stamps the host application
	// (Codex Desktop / CLI) and embedding context (vscode / terminal)
	// per session. We stash them in the matching schema columns since
	// they describe the same idea as the live-capture env fields.
	Terminal      string
	TerminalTitle string

	// Extras — arbitrary tool-specific metadata that has no neutral
	// home in InsertEntryInput. Sources stuff small string-keyed values
	// here (model_provider, sandbox_type, reasoning_effort, …) and the
	// driver encodes them as JSON into the `raw` column. Skipped when
	// nil/empty.
	Extras map[string]any
}

// Source is the per-tool import contract.
//
// Implementations must be cheap to construct (no I/O in the
// constructor) — Discover/Parse do all the work and are called by the
// driver only when the user actually runs `ailog import <name>`.
type Source interface {
	// Name is the user-facing subcommand identifier ("claude-code",
	// "codex", …). Must be stable; it appears in URLs / dedup keys.
	Name() string

	// DefaultRoot is where the source's transcripts live by default.
	// Returned as an OS-absolute path with $HOME expanded. Sources
	// should use a helper like expandHome("~/.claude/projects").
	DefaultRoot() string

	// Discover walks `root` and returns every transcript file the
	// source can parse. Order is irrelevant (the driver sorts by
	// mtime), but stability across calls is helpful for debugging.
	Discover(ctx context.Context, root string) ([]string, error)

	// Parse opens one file and emits Records via the callback. Returning
	// non-nil from emit aborts the parse early (used to support --limit).
	//
	// Sources MUST compute a stable LineHash per emitted record — the
	// driver will use it to dedup against the store's import_lines.
	Parse(ctx context.Context, path string, emit func(Record) error) error

	// LastKnownVersion is the highest upstream tool version this
	// parser has been validated against. Empty string disables the
	// drift watchdog. The driver compares this against the version
	// stamped on each emitted Record (Record.ToolVersion); when a
	// transcript reports a *higher* version AND the file's anchor
	// checks fail, the driver treats it as schema drift and skips
	// recording the file's mtime watermark so the next run retries.
	//
	// Bump this constant in the same PR that updates the parser to
	// handle a new transcript shape.
	LastKnownVersion() string

	// Anchor is the per-source "this row looks healthy" predicate used
	// to detect silent drift. The driver counts emitted Records per
	// file and how many of them passed Anchor; if Anchor never returns
	// true on a file with at least one *anchor-eligible* record, it
	// emits a warning ("zero rows passed the sanity check — likely
	// schema drift").
	//
	// Each source defines what "eligible" means by returning true only
	// for records it can confidently judge. A typical implementation
	// gates on Role==Assistant + TokensIn>0 — assistant records that
	// carry token usage are universally available across our supported
	// providers, so a file with assistants but zero usage suggests
	// usage.* got renamed upstream.
	Anchor(r Record) bool
}

// registry holds every registered Source by name. Concurrent reads
// dominate so an RWMutex would be overkill; the registry is populated
// at init() and never mutated thereafter.
var (
	regMu   sync.Mutex
	sources = map[string]Source{}
)

// Register adds a Source to the global registry. Panics on duplicate
// names (programming error — caught at startup).
func Register(s Source) {
	regMu.Lock()
	defer regMu.Unlock()
	name := s.Name()
	if _, ok := sources[name]; ok {
		panic(fmt.Sprintf("importer: duplicate source registration %q", name))
	}
	sources[name] = s
}

// Lookup returns the named source, or (nil, false) when missing.
func Lookup(name string) (Source, bool) {
	regMu.Lock()
	defer regMu.Unlock()
	s, ok := sources[name]
	return s, ok
}

// All returns every registered source, sorted by name. Useful for
// `ailog import all` and `--help` enumeration.
func All() []Source {
	regMu.Lock()
	defer regMu.Unlock()
	out := make([]Source, 0, len(sources))
	for _, s := range sources {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// Names returns the registered source names, sorted. For help text.
func Names() []string {
	regMu.Lock()
	defer regMu.Unlock()
	out := make([]string, 0, len(sources))
	for name := range sources {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
