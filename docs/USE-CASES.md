# ailog — use cases

Concrete scenarios where ailog earns its place. Organized by the
pain point it solves, not by feature.

- [Find a prompt you wrote last week](#find-a-prompt-you-wrote-last-week)
- [Build a personal prompt library](#build-a-personal-prompt-library)
- [Cross-tool knowledge (Claude + Codex + opencode)](#cross-tool-knowledge)
- [Token + cost accounting](#token--cost-accounting)
- [Redact what should never have hit disk](#redact-what-should-never-have-hit-disk)
- [Export a full conversation](#export-a-full-conversation)
- [Session hand-off](#session-hand-off)
- [Project audit / retrospective](#project-audit--retrospective)
- [Backfilling old transcripts](#backfilling-old-transcripts)
- [Web UI dashboarding](#web-ui-dashboarding)
- [Local-first, offline-safe](#local-first-offline-safe)

---

## Find a prompt you wrote last week

**The pain.** You wrote the right prompt to unblock yourself on a
gnarly race condition three weeks ago in Claude Code. Today you're
fighting the same class of bug in a different repo. You remember the
keywords but nothing else. Claude Code's in-session history doesn't
help — you closed that terminal long ago.

**The fix.**

```sh
ailog search "race condition goroutine" --since 30d
```

Every Claude Code / Codex / opencode turn you've captured in the last
30 days gets FTS-searched. Results show id prefix, timestamp, project,
and an 80-char prompt preview. Drill into one:

```sh
ailog show 019da628-579c
```

Full prompt + response with markdown rendering.

**Filter composition:**

```sh
ailog search "postgres index"           --project github.com/khanakia/api
ailog search "auth middleware"          --tool claude-code --branch feat/jwt
ailog search "typescript generics"      --since 90d --tool codex
ailog search "migration"                --session 019da607-dfac
```

All filters AND together. `--json` output if you're scripting against
it.

---

## Build a personal prompt library

**The pain.** You have ten prompts that consistently produce great
output: code review, commit message generator, "explain this code to
me like I'm the author," etc. Today you keep them in a Notes file and
copy-paste. The Notes file loses them over time and doesn't cross-
reference to the *actual outcome* when you first wrote them.

**The fix.** Star prompts directly in ailog:

```sh
ailog last 5                            # find the id
ailog star 019da628-579c
ailog tag  019da628-579c code-review,stable

ailog templates                          # every starred entry
```

The entry keeps its original context (project, branch, commit,
model, token count), so you remember *why* it worked when you're
looking at the template six months later. Tag by topic and filter:

```sh
ailog search --tag code-review
```

No copy-paste, no drift, no separate tool.

---

## Cross-tool knowledge

**The pain.** Your workflow spans Claude Code for planning, Codex for
boilerplate fill, opencode for some one-off scaffolding. Each tool
owns its own transcript format in its own corner of disk. None of
them talk to the others. A question you answered in Claude is
invisible from Codex the next day.

**The fix.** ailog imports from all three into one index:

```sh
ailog import claude-code   # live hook captures future turns; this backfills history
ailog import codex
ailog import opencode
ailog import all           # all of the above
```

Then search across the whole corpus regardless of which tool produced
each turn:

```sh
ailog search "denormalize" --tool codex       # codex turns only
ailog search "denormalize"                     # every tool
```

Stats by tool/model/project in the web UI. The token-cost breakdown
finally lets you see cumulative Codex usage alongside Claude Code
alongside opencode.

---

## Token + cost accounting

**The pain.** You have no idea which provider you spent more on last
month. Claude Code's UI shows per-session usage; Codex's shows
per-session too. No cross-session, no cross-project, no cross-tool
rollup. You can't answer "where did this month's token burn actually
go?" without manual spreadsheet work.

**The fix.** `ailog stats` aggregates automatically. Web UI at
`/stats` shows:

- Token-usage windows (all-time / last 30d / last 7d) with
  `commified` totals + compact form, plus input / output / cache-hit
  / cache-write breakdowns.
- Ranked tables "by tool", "by model", "by project" — each row shows
  count + ↓ in / ↑ out / ⟳ cache / total tokens in compact form, with
  a dot-grid mini-vis for relative scale.
- **Date-range picker** at the top (preset pills: All time / 24h /
  7d / 30d / 90d / Custom…) — change it and every card + every rank
  table re-aggregates against the selected range.

CLI surface: `ailog stats --json` emits the same aggregates as JSON
for scripts.

Gotcha worth knowing: cache-read tokens dwarf input on long sessions
(Codex / Claude Code cache most of the prompt context turn-over-turn)
— the stats page surfaces that as a separate column so you can
distinguish "billed input" from "cache-served input."

---

## Redact what should never have hit disk

**The pain.** You pasted a live AWS key into a prompt while debugging.
Auto-scrubbing caught it before write (AWS / GitHub / OpenAI / Anthropic
/ Slack tokens + JWTs + PEM blocks all redacted at capture). But now
you realize a *different* prompt three sessions ago contained a
customer email you shouldn't keep.

**The fix.**

```sh
ailog redact <id>           # replaces prompt/response/notes with "[redacted]"; metadata preserved
```

The id + project + timestamp stay so you can still see "this entry
existed and was scrubbed on X date." The FTS index re-syncs so the
sensitive text is no longer searchable. Irreversible — design
choice.

For bulk deletes:

```sh
ailog purge --before 2025-12-01 --yes
```

Hard-deletes every row and its FTS index. Confirm with the
`--yes` flag to match shell discipline.

---

## Export a full conversation

**The pain.** You want to share a multi-turn session with a colleague
or archive it as part of a post-mortem. Claude Code lets you export
individual messages. You want the whole session as a clean markdown
file with frontmatter.

**The fix.** In the web UI, on `/session/{id}`, there's a `Download
↓ Markdown` button. Or direct URL:

```
http://127.0.0.1:8090/session/019da607-dfac.md
```

Returns a markdown file with:

- YAML frontmatter (session id, name, tool, project, first/last turn
  time, turn count, aggregate tokens, models used)
- Per-turn sections `## Turn N · You` + `## Turn N · Assistant · <model>`
- `---` rules between turns
- Clean filename: `<slugified-session-name>-<shortid>.md` or
  `ailog-<shortid>.md` for unnamed sessions

Renders nicely in Obsidian, Notion, GitHub, or any standard markdown
viewer. CLI equivalent:

```sh
ailog export --format md --session 019da607-dfac > session.md
```

---

## Session hand-off

**The pain.** You're leaving a project mid-stream. Someone else is
picking it up. You want them to see not just the code but the AI
conversations that shaped the decisions — the prompts you tried, the
answers you got, the dead ends, the breakthroughs.

**The fix.** Name + export the relevant sessions:

```sh
ailog session name "auth redesign — rejected JWT, landed on OPAQUE PAT"
ailog session name "migration dry-run + rollback plan"
```

Then export each (`/session/{id}.md`) and drop the markdown files in
the repo's `docs/handoff/` directory alongside the code. The next
person opens them and reads the actual design conversation as it
happened.

---

## Project audit / retrospective

**The pain.** End of quarter. You want to answer "how much AI-assisted
work went into this project?" You vaguely remember a month of heavy
Claude Code usage on the auth rewrite but have no numbers.

**The fix.** The web UI's `/projects` page ranks projects by entry
count. Click through; the stats page filtered to that project shows
token burn, session count, first/last entry, per-model breakdown.

```sh
ailog stats --project github.com/you/your-project --since 90d --json
```

Plus the date-range picker on `/stats` lets you slice retroactively
— *"how much did we spend on Claude Opus in week 3?"* is a two-click
answer.

---

## Backfilling old transcripts

**The pain.** You installed ailog today but have months of Claude Code
/ Codex / opencode history on disk that you'd like searchable.

**The fix.**

```sh
ailog import all
```

Scans every known transcript root, parses each tool's native format,
normalizes into ailog's schema, dedup by per-line SHA so re-runs are
idempotent.

**Drift-safe.** Each importer declares a `LastKnownVersion` and an
`Anchor` predicate. When a tool ships a new transcript version and
the parser can no longer extract token counts, the watchdog warns:

```
WARN(newer-version) …jsonl: 12 assistant record(s), 0 carried token usage
— possible upstream schema drift (observed claude-code="3.0.0", parser known="2.1.114")
```

`--strict` escalates the warning to a hard reject (skips stamping
the mtime watermark, so a fixed parser re-walks the file on next
run).

See [USAGE.md](./USAGE.md#import) for the full surface.

---

## Web UI dashboarding

**The pain.** CLI is great for one-off lookups, terrible for "just
show me what's been happening."

**The fix.**

```sh
ailog ui
```

Opens `http://127.0.0.1:8090`. Pages:

| Route | What it is |
|---|---|
| `/` | Recent entries, newest first |
| `/search?q=…` | Live FTS (htmx-swap, sub-100ms) |
| `/table` | 35-column datagrid with per-column filters, sortable headers, column-chooser (localStorage-persisted), scroll-preserving pagination |
| `/sessions` | Paginated session list with filter + 6-mode sort (recent / newest / oldest / most-turns / fewest-turns / name) |
| `/session/{id}` | Threaded conversation, markdown-rendered, syntax-highlighted code; `Download ↓ Markdown` button |
| `/session/{id}.md` | Direct markdown download |
| `/entry/{id}` | Single-turn detail, full prompt + response, all metadata |
| `/projects` | Project leaderboard |
| `/templates` | Starred entries |
| `/stats` | Tokens + counts + per-tool/model/project breakdowns; date-range picker |

All server-rendered (Go + templ + htmx), localhost-only by default,
no JS framework, no build step, single binary. Auto-picks a free
port if 8090 is taken.

---

## Local-first, offline-safe

**The pain.** You don't want your AI conversation history going to a
cloud service. You also don't want to lose it when the laptop reboots
or the network drops.

**The fix.**

- `~/.ailog/log.db` is SQLite on your disk. No network syscalls on
  the read or write path except for optional `git` lookups to
  populate project metadata.
- `~/.ailog/` is `0700`, `log.db` is `0600`. Even other users on the
  same box can't read it.
- Zero telemetry. Zero outbound HTTP. `ailog ui` binds 127.0.0.1 by
  default; `--allow-network` required to expose on the LAN (and it
  has no auth, so you probably don't want that).
- Backups: just `cp ~/.ailog/log.db` somewhere. No database dump,
  no migration — plain SQLite file, portable to any machine.
- Offline: everything works without a network, including `ailog ui`
  and search. The only network calls are optional `git` lookups to
  canonicalize project names from remote URLs — trivially fails to
  `basename(cwd)` if offline.
