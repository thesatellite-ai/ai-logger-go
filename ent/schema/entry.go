// Package schema defines the ent schema. Each Entry row is one captured
// "turn" — a user prompt + (eventually) the assistant response that
// followed it. Most fields are populated at insert time from the
// hook-supplied JSON payload + auto-collected env; a few (response,
// usage, stop_reason, etc) are filled in later when the Stop hook fires.
//
// "Source" notes on each field below tell you which hook event /
// upstream tool produces the data, so you can tell at a glance whether
// it's universally populated or only present for, say, claude-code.
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
		field.String("id").
			Unique().
			Immutable().
			NotEmpty().
			Comment("UUID v7 generated at insert. Time-sortable, globally unique."),

		// ── Tool identity ─────────────────────────────────────────────
		field.String("tool").Default("").
			Comment(`Name of the AI tool that produced this turn.
Source: --tool flag on ailog add, or auto-set by per-tool hook adapter
(claude-code | codex | opencode | generic | manual).`),

		field.String("tool_version").Default("").
			Comment(`Version of the AI tool, if known.
Source: Claude Code transcript JSONL (top-level "version" field, e.g. "2.1.114").
Other tools: empty until their adapters extract it.`),

		// ── Project / repo context ────────────────────────────────────
		field.String("cwd").Default("").
			Comment("Working directory at capture time. Source: hook payload cwd OR auto-detected via os.Getwd()."),

		field.String("project").Default("").
			Comment(`Canonical "host/owner/repo" string derived from git remote.
Source: cwd → walk up to .git → read remote.origin.url → canonicalize.`),

		field.String("repo_owner").Default("").
			Comment("Repo owner (parsed from remote). Source: same as project."),

		field.String("repo_name").Default("").
			Comment("Repo name (parsed from remote). Source: same as project."),

		field.String("repo_remote").Default("").
			Comment("Raw remote.origin.url, before canonicalization."),

		field.String("git_branch").Default("").
			Comment("Current git branch at capture time. Source: `git rev-parse --abbrev-ref HEAD` in cwd."),

		field.String("git_commit").Default("").
			Comment("Short git SHA at capture time. Source: `git rev-parse --short HEAD`."),

		// ── Session ──────────────────────────────────────────────────
		field.String("session_id").Default("").
			Comment(`Identifier grouping turns from one conversation.
Source: hook payload session_id (claude-code: real Claude session UUID;
other tools: tool-supplied; manual: --session flag or freshly generated).`),

		field.String("session_name").Default("").
			Comment("User-assigned label for the session. Source: ailog session name CLI / web rename."),

		field.Int("turn_index").Default(0).
			Comment("0-based index within the session. Auto-computed by Store.InsertEntry."),

		field.String("parent_entry_id").Default("").
			Comment("ailog id of the previous turn in this session. Auto-linked at insert."),

		// ── Machine / shell context ──────────────────────────────────
		field.String("hostname").Default("").Comment("os.Hostname()."),
		field.String("user").Default("").Comment("$USER env var."),
		field.String("shell").Default("").Comment("basename of $SHELL env var."),
		field.String("terminal").Default("").Comment("$TERM_PROGRAM env var (iTerm.app, ghostty, …)."),
		field.String("terminal_title").Default("").
			Comment("Best-effort terminal title (env-only — see internal/context/env.go)."),
		field.String("tty").Default("").Comment("Controlling tty path."),
		field.Int("pid").Default(0).
			Comment("Parent process id ($AILOG_PARENT_PID env or os.Getppid())."),

		// ── Payload ──────────────────────────────────────────────────
		field.Text("prompt").Default("").
			Comment("The user's prompt text, secret-scrubbed. Source: hook prompt field / --prompt flag / stdin."),

		field.Text("response").Default("").
			Comment(`The assistant's response text, secret-scrubbed.
Source: claude-code Stop hook — prefers payload.last_assistant_message
(race-free) and falls back to parsing the transcript jsonl.`),

		field.String("model").Default("").
			Comment(`Model identifier (e.g. "claude-opus-4-7").
Source: Claude Code transcript message.model. Other tools: --model flag.`),

		field.Text("raw").Default("").
			Comment(`Free-form provenance blob. Currently used for:
- claude-code: stores transcript_path so we can re-derive metadata if needed.
- ailog import: SHA-256 hash of the source JSONL line, for idempotent backfill.`),

		// ── Usage / runtime metadata (the new "Tier 1" columns) ──────
		field.Int("token_count_in").Default(0).
			Comment(`Input tokens billed for this turn.
Source: Claude Code Stop hook — transcript message.usage.input_tokens.
Other tools: 0 unless adapter populates it.`),

		field.Int("token_count_out").Default(0).
			Comment(`Output tokens generated by the assistant for this turn.
Source: Claude Code Stop hook — transcript message.usage.output_tokens.`),

		field.Int("token_count_cache_read").Default(0).
			Comment(`Tokens served from Anthropic's prompt cache (cache HIT).
Source: Claude Code Stop hook — transcript message.usage.cache_read_input_tokens.
High value = cache hit %; cheap turns. Anthropic-specific (zero for non-Claude tools).`),

		field.Int("token_count_cache_create").Default(0).
			Comment(`Tokens written into Anthropic's prompt cache (cache MISS / write).
Source: Claude Code Stop hook — transcript message.usage.cache_creation_input_tokens.
Anthropic-specific.`),

		field.String("stop_reason").Default("").
			Comment(`Why the assistant stopped this turn.
Source: Claude Code Stop hook — transcript message.stop_reason.
Common values: "end_turn" (normal), "tool_use" (mid-turn tool call),
"max_tokens" (hit output cap), "stop_sequence". Distinguishes complete
responses from interrupted ones for filtering / debugging.`),

		field.String("permission_mode").Default("").
			Comment(`Claude Code permission mode at the moment of capture.
Source: Claude Code hook payload "permission_mode" field
(UserPromptSubmit and Stop both carry it).
Values: "default" | "acceptEdits" | "bypassPermissions" | "plan".
Lets you filter "everything I did in plan mode" or audit YOLO sessions.`),

		// ── Curation ─────────────────────────────────────────────────
		field.String("tags").Default("").
			Comment("CSV of user-applied tags. Edited via ailog tag / web tag form."),

		field.Bool("starred").Default(false).
			Comment("User flag for templates / keepers."),

		field.Text("notes").Default("").
			Comment("Free-form user annotation, markdown supported."),

		// ── Timestamps ───────────────────────────────────────────────
		field.Time("created_at").Default(time.Now).Immutable().
			Comment("Insert wall time (UTC)."),
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
