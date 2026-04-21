---
name: ailog
description: Local persistent log of AI prompts and responses on this machine. Use when the user asks to recall past prompts ("what did I ask about X", "find that prompt from last week", "did we ever discuss auth"), curate history ("star that", "tag that", "name this session"), inspect recent turns ("show last 5 prompts", "session stats"), export a session ("dump this conversation as markdown"), or scrub sensitive content ("redact entry X", "purge before Y"). Capture is handled automatically by hooks — do NOT call `ailog add` / `ailog init` / `ailog hooks install` / `ailog skill install` from this skill under any circumstance.
---

# ailog — persistent conversation log

`ailog` is a local Go CLI on PATH (SQLite + FTS5). It records every
prompt + response from Claude Code, Codex, and opencode **automatically**
via `UserPromptSubmit` and `Stop` hooks. Your job in this skill is
**recall, browse, curate, export, and scrub** — never capture.

## Never do

- **`ailog add …`** — capture is handled by hooks. Calling it creates a
  duplicate row.
- **`ailog init`, `ailog hooks install`, `ailog hooks uninstall`,
  `ailog skill install`, `ailog migrate`** — one-time user setup. Don't
  run them unsolicited even if the user sounds confused about setup;
  refer them to the README instead.
- **`ailog import …`** — backfill from on-disk transcripts. Large,
  long-running, touches the whole DB. Run only if the user explicitly
  says "import my old claude/codex/opencode history."

## Intent dispatcher

Read the user's request and jump straight to the matching branch:

| User says (patterns) | Go to |
|---|---|
| "what did I ask about", "find that prompt", "did I ever ask", "search for" | [Recall](#1-recall-past-prompts) |
| "last N prompts", "recent", "what did I just do" | [Recent](#2-recent-history) |
| "show me the session", "walk through the conversation", "replay" | [Session browse](#3-session-browse--export) |
| "export … as markdown", "download", "save the conversation" | [Session export](#3-session-browse--export) |
| "save / star / pin / bookmark this", "tag that", "label" | [Curate](#4-curate) |
| "how many prompts", "token usage", "stats" | [Stats](#5-stats) |
| "redact", "scrub", "delete that entry", "purge before X" | [Redact / purge](#6-redact--purge) |
| "open the web UI", "browse in a browser" | `ailog ui` — prints URL, then stop; let the user click |

## 1. Recall past prompts

Use FTS5 keyword search, always with `--json` for machine parsing.

```bash
ailog search "<keywords>" --json --limit 20
```

**JSON fields you'll read:** `id`, `created_at`, `tool`, `model`,
`project`, `git_branch`, `session_id`, `prompt`, `response` (possibly
truncated), `token_count_in`, `token_count_out`.

**Refine with filters** — combine freely, they AND together:

```bash
ailog search "race" --project github.com/khanakia/ai-logger --since 7d --json
ailog search "auth" --tool claude-code --branch feat/jwt --json
ailog search "migration" --session 019da607-dfac --json
```

Flags: `--project`, `--tool`, `--session`, `--branch`, `--since` (accepts
`7d`, `2w`, `24h`, `30m`, or an RFC3339 date), `--limit`, `--json`.

### Worked example

> User: "what did I ask about postgres indexes last week?"

```bash
ailog search "postgres index" --since 7d --json --limit 10
```

Read the JSON. Present 2–5 matches inline, one line each, with short
prompt preview + id prefix:

> Found 3 prompts about postgres indexes in the last 7 days:
>
> 1. `019da628-579c` · 2d ago · ai-logger · main
>    *"why is this btree index not used on range scans"*
> 2. `019da5f0-12b1` · 3d ago · ai-logger · feat/reports
>    *"should I add a GIN index on the tags column"*
> 3. `019da430-8b3f` · 6d ago · repolink · main
>    *"compare hash vs btree for exact-match lookup"*
>
> Want the full prompt + response for any of these?

If user picks one:

```bash
ailog show 019da628-579c
```

First 13 characters of the id are almost always unique — pass that.
Errors loudly on ambiguous prefix.

## 2. Recent history

```bash
ailog last 10              # last 10 entries (default 5)
ailog last 10 --json       # when you need to parse before presenting
```

Use when user says *"what did I just do"*, *"recap my last few prompts"*,
*"catch me up"*. Summarize chronologically, newest first — ids +
one-line prompt previews, grouped by session if >1 session appears.

## 3. Session browse + export

Every turn is tagged with a `session_id`. Pull one whole thread with:

```bash
ailog session show <session-id>             # prints all turns in order
ailog session show <session-id> --json      # same, machine-readable
```

For replay/summary: ingest the full thread, summarize what was
discussed + final outcome in 3–5 bullets. Preserve the assistant's
actual conclusions — don't paraphrase away concrete answers.

### Export the whole session as markdown

```bash
# CLI:
ailog export --format md --session <session-id> > session.md

# Or point the user at the web download:
ailog ui                                      # server already running? skip this
# Then open http://127.0.0.1:8090/session/<session-id>.md
```

The web download produces a richer markdown (YAML frontmatter with
session metadata + per-turn sections). Prefer it when the user wants a
shareable file.

## 4. Curate

| User says | Run |
|---|---|
| "save that as a template", "star that prompt", "pin it", "bookmark" | `ailog star <id>` |
| "unstar that" | `ailog unstar <id>` |
| "show me my templates / starred / saved prompts" | `ailog templates` |
| "tag that with X, Y" | `ailog tag <id> X,Y` (comma-sep; merges with existing) |
| "name this session 'debugging auth'" | `ailog session name "debugging auth"` |
| "rename session X to Y" | `ailog session name "Y"` (while in that session) |

### Disambiguation pattern

When the user refers to *"the last prompt"* / *"that one"* without an
id, first run `ailog last 5 --json`, identify the row by timestamp +
content match, then act on its id. Confirm back to the user:
*"Starred turn `019da628-579c` — 'why is this btree index not used...'"*.

## 5. Stats

```bash
ailog stats                 # summary: counts per tool, model, project, session
ailog stats --json          # machine-readable
```

The JSON includes time-windowed token aggregates (`tokens_all_time`,
`tokens_30d`, `tokens_7d`, each with `in`/`out`/`cache_read`/
`cache_write`/`entries`) plus per-group breakdowns (`tokens_by_tool`,
`tokens_by_model`, `tokens_by_project`).

Use when user asks about:

- raw counts: *"how many prompts have I logged"*, *"how many sessions"*
- token usage / cost: *"how much have I spent on claude this week"*,
  *"token burn this month"*
- breakdown by project / tool: *"where did most of my prompts go"*

If the user wants a visual, point them at `http://127.0.0.1:8090/stats`
(the bundled web UI) — it has dot-bar rank tables and a token-usage
card stack that reads better than terminal text.

## 6. Redact / purge

| User says | Run |
|---|---|
| "redact entry X", "scrub that prompt" | `ailog redact <id>` (replaces prompt/response with `[redacted]`, keeps metadata + id) |
| "delete everything before 2026-01-01" | `ailog purge --before 2026-01-01 --yes` |

**Before running `purge`, confirm**: show the user how many rows will be
deleted. Use `ailog search "" --since <cutoff>` as a rough count preview
or run `ailog stats --json` before/after.

Auto-scrubbing of common secret shapes (AWS / GitHub / OpenAI /
Anthropic / Slack tokens, JWTs, PEM private keys) runs on every
capture — you don't need to manually scrub for those.

## Finding the right id (disambiguation)

- `ailog last 5` and `ailog search` both print 13-char id prefixes.
- Pass the prefix directly to `show`/`redact`/`tag`/`star` — the CLI
  resolves it and errors loudly on ambiguity.
- If the user describes a turn by content but can't remember the id,
  search for the content: `ailog search "<their description>" --json`
  → pick the row whose prompt/response best matches → use its id.

## Output formatting rules

- **Never paste raw JSON to the user.** Use `--json` only for your own
  parsing. Present results as prose + inline code-fenced ids/prompts.
- **Show short previews, not full bodies** unless the user asked to
  "show" a specific entry. ~80-char prompt preview per result is ideal.
- **Always include the id prefix** so the user can say "that one" and
  you can act on it.
- **Group multiple-session results by session** when presenting >5
  results — cuts repetition of session metadata.
- **Relative timestamps** ("2d ago", "5h ago") read better than
  absolute ones for recall. Compute from `created_at` + current time.
- **Don't editorialize** the prompt/response content — surface what the
  user actually said.

## Gotchas

- **Session id vs entry id** — different. `session_id` is the parent
  conversation (`019da607-...`); entry id is the per-turn UUID (also
  `019da...` but distinct). `ailog session show` takes session id;
  everything else takes entry id.
- **`--since` is inclusive at the start, exclusive at the end** — a
  prompt at `--since 7d` boundary may or may not appear; don't rely on
  exact boundary.
- **Project "(none)"** — shows up in stats when the turn was captured
  outside a git repo AND the cwd basename fallback was somehow empty.
  Treat as "no project context", don't surface to the user unless
  they're debugging.
- **Token counts are 0 for non-Anthropic tools** on older entries — not
  a bug, just that codex/opencode didn't expose usage until recent
  importer work. Frame as "no usage data for this entry."
- **`ailog show` can print very long responses** — always trim in your
  summary. If user wants the full thing, send them to the web UI at
  `http://127.0.0.1:8090/entry/<id>`.

## Fast reference

```bash
ailog last [N]                                 # recent entries
ailog search <q> [--json]                      # FTS5 search
ailog search <q> --project <p> --since 7d      # filter combo
ailog show <id-prefix>                         # full entry
ailog session show <session-id>                # all turns in a session
ailog session name "<label>"                   # label current session
ailog tag <id> a,b                             # add tags (merges)
ailog star <id> | ailog unstar <id>            # mark / unmark
ailog templates                                # starred entries
ailog stats [--json]                           # counts + token windows
ailog export --format md|json --session <id>   # export one conversation
ailog redact <id>                              # scrub bodies, keep id
ailog purge --before <date> --yes              # bulk delete (destructive)
ailog ui                                       # browser UI
```

Run `ailog --help` for the full surface.
