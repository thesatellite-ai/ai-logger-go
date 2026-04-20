// Package schema defines the ent schema. Each Entry row is one captured
// "turn" — a user prompt + (eventually) the assistant response that
// followed it. Rows arrive through three different paths:
//
//	1. Live hooks            (internal/cli/hook_*.go)  — UserPromptSubmit
//	                          inserts a prompt-only row, Stop fills in the
//	                          response + token usage. Currently real for
//	                          claude-code; codex/opencode skeletons exist.
//	2. Manual capture        (`ailog add`)              — single insert from
//	                          flags / stdin.
//	3. Backfill import       (internal/importer/*.go)   — walks each tool's
//	                          on-disk transcript tree and replays them
//	                          through the same store APIs the hooks use.
//
// "Source" notes on each field below tell you which path / upstream tool
// produces the data, so you can tell at a glance whether it's universally
// populated or only present for, say, claude-code. Three-character
// shorthand:
//
//	[live]  — set at live-hook capture time
//	[ev]    — auto-collected from env (cwd, git, hostname, …)
//	[imp]   — populated by `ailog import <tool>` from the on-disk transcript
package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Entry is one captured prompt+response pair plus the rich context we
// auto-collect at the moment of capture.
type Entry struct {
	ent.Schema
}

func (Entry) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "entries"},
	}
}

// Fields are grouped by source / lifecycle:
//
//	identity:        id
//	tool identity:   tool, tool_version
//	project / repo:  cwd, project, repo_owner, repo_name, repo_remote, git_branch, git_commit
//	session:         session_id, session_name, turn_index, parent_entry_id
//	machine / shell: hostname, user, shell, terminal, terminal_title, tty, pid
//	payload:         prompt, response, model, raw
//	usage / runtime: token_count_in, token_count_out, token_count_cache_read,
//	                 token_count_cache_create, stop_reason, permission_mode
//	curation:        tags, starred, notes
//	timestamps:      created_at
func (Entry) Fields() []ent.Field {
	return []ent.Field{
		// ── Identity ──────────────────────────────────────────────────
		// UUID v7 generated at insert. Time-sortable, globally unique.
		field.String("id").
			Unique().
			Immutable().
			NotEmpty(),

		// ── Tool identity ─────────────────────────────────────────────
		// Name of the AI tool that produced this turn. Stable string used
		// for grouping in /stats and as a filter facet in /table.
		// Source:
		//   [live] per-tool hook adapter sets it ("claude-code" | "codex" | "opencode" | "generic")
		//   [imp]  importer.Source.Name() — same string, since import + hook share the taxonomy
		//   manual: --tool flag on `ailog add` (default "manual")
		field.String("tool").Default(""),

		// Version of the AI tool, if known. Free-form per tool — we don't
		// parse it, just stamp it for filtering / debugging.
		// Source:
		//   [live] claude-code: transcript JSONL top-level "version" (e.g. "2.1.114")
		//   [imp]  claude-code: same field
		//   [imp]  codex: session_meta.cli_version (e.g. "0.118.0-alpha.2")
		//   [imp]  opencode: session.json "version" (e.g. "0.14.7")
		//   other tools: empty until their adapters extract it.
		field.String("tool_version").Default(""),

		// ── Project / repo context ────────────────────────────────────
		// Working directory at capture time. Drives every other field in
		// this section (project / repo_* / git_*) by being the path we
		// shell git out against.
		// Source:
		//   [live] hook payload cwd (claude-code: "cwd"; codex: turn_context.cwd) → fallback os.Getwd()
		//   [imp]  claude-code: transcript line "cwd"
		//   [imp]  codex: session_meta.cwd / per-turn turn_context.cwd
		//   [imp]  opencode: msg.path.cwd → fallback session.directory
		field.String("cwd").Default(""),

		// Canonical "host/owner/repo" string derived from git remote, with
		// a sensible fallback so non-repo cwds still get a non-empty value
		// (otherwise /projects + stats group everything under "(none)").
		// Source: cwd → CollectGit → CanonicalProject(remote) (host/owner/repo)
		//                          → if empty, basename(cwd)
		// Same logic in [live] (internal/context/collect.go) and [imp]
		// (internal/importer/run.go resolveProject).
		field.String("project").Default(""),

		// Repo owner (parsed from remote). Source: same as project.
		field.String("repo_owner").Default(""),

		// Repo name (parsed from remote). Source: same as project.
		field.String("repo_name").Default(""),

		// Raw remote.origin.url, before canonicalization.
		field.String("repo_remote").Default(""),

		// Current git branch at capture time. Source: `git rev-parse --abbrev-ref HEAD` in cwd.
		field.String("git_branch").Default(""),

		// Short git SHA at capture time. Source: `git rev-parse --short HEAD`.
		field.String("git_commit").Default(""),

		// ── Session ──────────────────────────────────────────────────
		// Identifier grouping turns from one conversation. Different tools
		// use different id shapes; ailog never re-keys, just stores.
		// Source:
		//   [live] hook payload session_id (claude-code: real Claude session UUID)
		//   [imp]  claude-code: transcript "sessionId" (UUID)
		//   [imp]  codex:  session_meta.id (rollout UUID like "019d2a98-7e27-…")
		//   [imp]  opencode: session.id ("ses_xxx" — opencode's own format)
		//   manual: --session flag or freshly generated
		field.String("session_id").Default(""),

		// Human-friendly label for the session. Backfilled by import for
		// tools that name sessions natively; for live capture it's set by
		// the user via web rename / `ailog session name`.
		// Source:
		//   [imp]  opencode: session.title ("Building next.js app with Drizzle database")
		//   [live] /session/{id}/name endpoint or `ailog session name`
		//   claude-code / codex: empty until renamed (tools don't title sessions)
		field.String("session_name").Default(""),

		// 0-based index within the session. Auto-computed by Store.InsertEntry.
		field.Int("turn_index").Default(0),

		// ailog id of the previous turn in this session. Auto-linked at insert.
		field.String("parent_entry_id").Default(""),

		// ── Machine / shell context ──────────────────────────────────
		// All [live]+[ev] sourced from internal/context/env.go at hook
		// fire time. [imp] backfill uses these slots to stash analogous
		// per-tool host / embedding metadata when it's available, since
		// the original capture-machine fields are unrecoverable from
		// historical transcripts.
		//
		// os.Hostname(). [live] only — empty on imported rows.
		field.String("hostname").Default(""),
		// $USER env var. [live] only.
		field.String("user").Default(""),
		// basename of $SHELL env var. [live] only.
		field.String("shell").Default(""),
		// $TERM_PROGRAM env var (iTerm.app, ghostty, …) at live capture.
		// Source:
		//   [live] env $TERM_PROGRAM
		//   [imp]  codex:  session_meta.originator ("Codex Desktop", "Codex CLI")
		//                  — repurposed for the host application name
		//   claude-code / opencode import: empty
		field.String("terminal").Default(""),
		// Best-effort terminal title at live capture (env-only — see
		// internal/context/env.go). Repurposed by codex import.
		// Source:
		//   [live] env-derived title
		//   [imp]  codex: session_meta.source ("vscode" / "terminal")
		field.String("terminal_title").Default(""),
		// Controlling tty path. [live] only.
		field.String("tty").Default(""),
		// Parent process id ($AILOG_PARENT_PID env or os.Getppid()). [live] only.
		field.Int("pid").Default(0),

		// ── Payload ──────────────────────────────────────────────────
		// The user's prompt text, run through internal/redact before
		// storage so secrets the user typed don't land in the FTS index.
		// Source:
		//   [live] hook payload .prompt (claude-code: "prompt" string)
		//   [imp]  claude-code: transcript user-line message.content (string OR text-block array)
		//   [imp]  codex: event_msg.user_message.message
		//   [imp]  opencode: concatenation of text-typed parts under storage/part/<msgID>/
		//   manual: --prompt flag / stdin
		field.Text("prompt").Default(""),

		// The assistant's response text, scrubbed the same way as prompt.
		// Source:
		//   [live] claude-code Stop hook — prefers payload.last_assistant_message
		//          (race-free), falls back to scanning the transcript jsonl
		//   [imp]  claude-code: transcript assistant-line message.content text blocks
		//   [imp]  codex: event_msg.agent_message.message (phase=final_answer only)
		//   [imp]  opencode: concatenation of text-typed parts; tool/reasoning/step-* parts ignored
		field.Text("response").Default(""),

		// Model identifier. Free-form because each tool names models
		// differently — we just stamp the string verbatim.
		// Source:
		//   [live] claude-code Stop: transcript message.model ("claude-opus-4-7")
		//   [imp]  claude-code: same field
		//   [imp]  codex: turn_context.model ("gpt-5.4")
		//   [imp]  opencode: assistant msg.modelID ("gpt-5-nano", "gemma4")
		//   manual: --model flag
		field.String("model").Default(""),

		// Free-form provenance blob. Two distinct shapes coexist (the
		// importer dedup primitive moved to the import_lines table, so
		// raw is no longer load-bearing for idempotency):
		//   [live] claude-code: transcript_path string so we can re-scan
		//          for metadata if needed
		//   [imp]  claude-code: bare SHA-256 of the source JSONL line
		//   [imp]  codex / opencode: small JSON object —
		//          {"line_hash":"…","codex.model_provider":"openai",
		//           "codex.sandbox_type":"workspace-write",
		//           "codex.network_access":false,"codex.collab_mode":"default",
		//           "codex.reasoning_effort":"medium","codex.personality":"…",
		//           "codex.timezone":"…"}
		//          {"line_hash":"…","opencode.provider_id":"openai",
		//           "opencode.agent":"build","opencode.mode":"build",
		//           "opencode.project_id":"global","opencode.cost_usd":0.00119}
		//          Renderers should treat raw as opaque unless they need
		//          the namespaced extras.
		field.Text("raw").Default(""),

		// ── Usage / runtime metadata (the "Tier 1" columns) ──────────
		// Driven by the live Stop hook (claude-code) and by the importer
		// for backfilled rows. Each tool's wire format gets normalized
		// onto these four ints + the two free-form strings below.
		//
		// Cross-tool mapping:
		//   in              out                    cache_read              cache_create
		//   ───             ───────────            ───────────────────     ────────────────
		//   claude usage:   input_tokens           output_tokens           cache_read_input_tokens
		//                                                                  / cache_creation_input_tokens
		//   codex usage:    input_tokens           output_tokens
		//                                          + reasoning_output_tokens   cached_input_tokens   (no write)
		//   opencode tokens: input                 output + reasoning      cache.read              cache.write
		//
		// Reasoning tokens are folded into "out" because providers bill
		// them as output — keeping the two separate would understate
		// per-turn output cost in /stats.

		// Input tokens billed for this turn.
		// Source:
		//   [live] claude-code Stop: transcript usage.input_tokens
		//   [imp]  claude-code: same; codex: token_count.info.last_token_usage.input_tokens
		//   [imp]  opencode: msg.tokens.input
		field.Int("token_count_in").Default(0),

		// Output tokens generated by the assistant for this turn,
		// reasoning included.
		// Source:
		//   [live] claude-code Stop: transcript usage.output_tokens
		//   [imp]  claude-code: same
		//   [imp]  codex: usage.output_tokens + usage.reasoning_output_tokens
		//   [imp]  opencode: msg.tokens.output + msg.tokens.reasoning
		field.Int("token_count_out").Default(0),

		// Tokens served from a cache (cache HIT). Originally Anthropic-only;
		// codex & opencode use the same column for their analogous cached-
		// input metric so cross-tool grouping in /stats stays meaningful.
		// Source:
		//   [live] claude-code Stop: transcript usage.cache_read_input_tokens
		//   [imp]  claude-code: same
		//   [imp]  codex: token_count.info.last_token_usage.cached_input_tokens
		//   [imp]  opencode: msg.tokens.cache.read
		field.Int("token_count_cache_read").Default(0),

		// Tokens written into a prompt cache (cache MISS / write).
		// Source:
		//   [live] claude-code Stop: transcript usage.cache_creation_input_tokens
		//   [imp]  claude-code: same
		//   [imp]  opencode: msg.tokens.cache.write
		//   codex: 0 (cli rollouts don't expose a cache-write counter)
		field.Int("token_count_cache_create").Default(0),

		// Why the assistant stopped this turn. Used to distinguish
		// completed answers from mid-turn tool calls / token-cap cuts.
		// Source:
		//   [live] claude-code Stop: transcript message.stop_reason
		//   [imp]  claude-code: same
		//   Values seen: "end_turn" | "tool_use" | "max_tokens" | "stop_sequence"
		//   codex / opencode: empty (rollouts don't surface a stop reason)
		field.String("stop_reason").Default(""),

		// Authorization / sandbox mode active when the turn happened.
		// Re-used across tools for the same idea ("how locked-down was
		// the agent?"), even though each tool's vocabulary differs.
		// Source:
		//   [live] claude-code: hook payload "permission_mode"
		//          ("default" | "acceptEdits" | "bypassPermissions" | "plan")
		//   [imp]  claude-code: same
		//   [imp]  codex: turn_context.approval_policy
		//          ("on-request" | "never" | "unless-trusted")
		//   opencode: empty (no analogous knob exposed yet)
		field.String("permission_mode").Default(""),

		// ── Curation ─────────────────────────────────────────────────
		// User-driven; not populated by hooks or imports.
		// CSV of user-applied tags. Edited via ailog tag / web tag form.
		field.String("tags").Default(""),

		// User flag for templates / keepers.
		field.Bool("starred").Default(false),

		// Free-form user annotation, markdown supported.
		field.Text("notes").Default(""),

		// ── Timestamps ───────────────────────────────────────────────
		// When the turn happened.
		// Source:
		//   [live] default time.Now() at insert (Immutable())
		//   [imp]  importer overrides via store.InsertEntryInput.CreatedAt:
		//          claude-code: transcript line "timestamp" (RFC3339Nano UTC)
		//          codex: envelope timestamp (RFC3339Nano UTC)
		//          opencode: msg.time.created (unix ms → UTC)
		// Immutable() prevents updates, but Create accepts an explicit
		// SetCreatedAt(t) — that's how backfill keeps historical dates.
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (Entry) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project", "created_at"),
		index.Fields("tool", "created_at"),
		index.Fields("session_id", "turn_index"),
		index.Fields("cwd", "created_at"),
		index.Fields("git_branch", "created_at"),
		index.Fields("starred"),
		index.Fields("permission_mode", "created_at"), // filter "show me plan-mode entries"
	}
}
