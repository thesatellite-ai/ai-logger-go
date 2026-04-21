# ailog — usage reference

Every CLI command with flags and examples. For scenario-driven
documentation see [USE-CASES.md](./USE-CASES.md); for the project
overview see the main [README](../README.md).

- [Installation + setup](#installation--setup)
- [Capture (manual + hooks)](#capture-manual--hooks)
- [Search + recall](#search--recall)
- [Session ops](#session-ops)
- [Curation (star / tag / notes)](#curation-star--tag--notes)
- [Reports + stats](#reports--stats)
- [Import (backfill)](#import-backfill)
- [Privacy (redact / purge)](#privacy-redact--purge)
- [Export](#export)
- [Web UI](#web-ui)
- [Migrate / debug](#migrate--debug)
- [Environment](#environment)
- [JSON field reference](#json-field-reference)

---

## Installation + setup

### Install

```sh
curl -sL https://raw.githubusercontent.com/thesatellite-ai/ai-logger-go/main/install.sh | sh
```

Windows: `irm https://raw.githubusercontent.com/thesatellite-ai/ai-logger-go/main/install.ps1 | iex`

### First-time init

```sh
ailog init                                 # creates ~/.ailog/log.db + applies migrations
ailog hooks install --tool claude-code     # writes hook entries into ~/.claude/settings.json
ailog skill install                        # writes SKILL.md → ~/.claude/skills/ailog/
```

`--tool` is required on `hooks install` — no default. Known tools:
`claude-code` (real, auto-installs into Claude's settings.json),
`codex` + `opencode` (skeleton adapters; see [USE-CASES → backfill](./USE-CASES.md#backfilling-old-transcripts)).

Alternative skill install via skills.sh:

```sh
npx skills add thesatellite-ai/ai-logger-go
```

### Uninstall

```sh
ailog hooks uninstall --tool claude-code   # surgical — only removes ailog entries from settings.json
sudo rm /usr/local/bin/ailog
rm -rf ~/.ailog
```

The `uninstall` defaults to `claude-code` (unlike `install`) because
by the time you uninstall you've committed to the tool.

---

## Capture (manual + hooks)

Capture is automatic via hooks for claude-code. For other tools,
manual capture or backfill via `ailog import`.

### `ailog add`

```sh
ailog add --prompt "why is this btree index not used"
ailog add --tool codex --session $SID --prompt "$(cat prompt.md)"
cat prompt.md | ailog add --tool cursor     # stdin works
ailog add --response "..." --entry <id>      # attach response later (two-step)
```

| Flag | Meaning |
|---|---|
| `--tool <name>` | Tool identifier. Default: empty (treated as `manual` by the UI) |
| `--session <id>` | Group into a session. Auto-generated if absent |
| `--prompt <text>` | User prompt. Accepts `-` for stdin |
| `--response <text>` | Assistant response (used with `--entry` or directly at insert) |
| `--entry <id>` | Attach response to an existing entry (two-step capture) |
| `--model <id>` | Model identifier |
| `--tags <csv>` | Initial tag set |
| `--no-redact` | Skip secret scrubber (use with care) |

Output: the new entry's id (UUID v7, time-sortable).

### `ailog hooks`

```sh
ailog hooks list                           # every tool adapter ailog knows about
ailog hooks show --tool claude-code        # print the commands the hooks will invoke
ailog hooks install --tool claude-code     # writes into ~/.claude/settings.json
ailog hooks uninstall                      # defaults to --tool claude-code
```

`install` is surgical-**write** (merges with existing `hooks{}` in
`settings.json` without clobbering other tools' entries). `uninstall`
is surgical-**delete** (walks `hooks[event][].hooks[]` and drops only
entries whose `command` contains `" hook "` + `"ailog"` — other tools
preserved).

See `ailog hook --help` for the internal adapter surface — you
rarely invoke these directly; the harness does via the hook config.

---

## Search + recall

### `ailog search`

```sh
ailog search "race condition"
ailog search "auth" --project github.com/khanakia/api --since 7d
ailog search "migration" --tool claude-code --branch feat/schema-v2
ailog search "typescript" --tag debugging --limit 50 --json
```

| Flag | Meaning |
|---|---|
| `--project <p>` | Exact project match (canonical `host/owner/repo` or basename) |
| `--tool <t>` | Exact tool match |
| `--session <id>` | Filter to one session |
| `--branch <b>` | Filter to one git branch |
| `--tag <t>` | Filter to entries with this tag |
| `--since <dur>` | Rolling window — `7d`, `2w`, `24h`, `30m`, or RFC3339 date |
| `--limit <n>` | Cap results (default 20) |
| `--json` | Machine-readable output |

Backed by SQLite FTS5. Query syntax supports BM25 ranking, prefix
matching (`"rac*"`), phrase queries (`"\"race condition\""`).

### `ailog show`

```sh
ailog show 019da628-579c          # 13-char prefix is almost always unique
ailog show 019da628-579c --json
```

Renders full prompt + response with markdown + syntax highlighting.
Errors loudly on ambiguous prefix.

### `ailog last`

```sh
ailog last                # 5 most recent
ailog last 20             # 20 most recent
ailog last 10 --json      # machine-readable
```

---

## Session ops

```sh
ailog session show                       # turns in the current (auto-detected) session
ailog session show 019da607-dfac         # specific session
ailog session show 019da607-dfac --json  # JSON per turn
ailog session name "auth redesign"       # rename the current session
ailog session id                         # print the current session id
```

The current session is resolved from `AILOG_SESSION_ID` env + the
session inferred from the most recent entry in this cwd.

---

## Curation (star / tag / notes)

```sh
ailog star   019da628-579c       # mark as template / keeper
ailog unstar 019da628-579c
ailog templates                  # list every starred entry

ailog tag    019da628-579c debugging,concurrency   # merges + de-dupes + sorts
ailog tag    019da628-579c -r legacy-tag           # remove a tag

# notes: free-form markdown annotation, indexed by FTS
ailog notes  019da628-579c "this was the prompt that unblocked the goroutine leak"
```

Tags are CSV-stored, edited via merge semantics. Notes are free-form
markdown and indexed by FTS alongside prompt/response.

---

## Reports + stats

### `ailog stats`

```sh
ailog stats             # formatted table output
ailog stats --json      # machine-readable; feeds the /stats web view
```

The JSON carries:

- Counts: `total`, `starred`, `distinct_sessions`
- Per-group count maps: `by_tool`, `by_model`, `by_project`
- Three fixed token windows: `tokens_all_time`, `tokens_30d`,
  `tokens_7d` (each with `in`/`out`/`cache_read`/`cache_write`/
  `entries`)
- Per-group token aggregates: `tokens_by_tool`, `tokens_by_model`,
  `tokens_by_project` (same `TokenWindow` shape)
- First / last entry timestamps

### Date-range filter (web UI)

The web `/stats` page has a date-range picker — preset pills
(`All time · 24h · 7d · 30d · 90d · Custom…`) plus two native
`<input type="date">` inputs. URL query:

```
http://127.0.0.1:8090/stats?from=2026-04-01&to=2026-04-21
```

Every card + rank table re-aggregates against the filter. Empty
query → all-time (current behavior). Invalid / inverted ranges →
degrade to all-time rather than erroring.

---

## Import (backfill)

Pull historical transcripts into ailog. Idempotent via per-line SHA
dedup + per-file mtime watermarks — re-runs are fast.

```sh
ailog import claude-code      # walks ~/.claude/projects/**/*.jsonl
ailog import codex            # walks ~/.codex/sessions/**/*.jsonl
ailog import opencode         # walks ~/.local/share/opencode/storage
ailog import all              # every registered source
```

| Flag | Meaning |
|---|---|
| `--from <path>` | Override the source's default root directory |
| `--since <RFC3339>` | Skip records older than this timestamp |
| `--limit <n>` | Stop after importing N records |
| `--force` | Ignore per-file mtime watermark; reparse everything |
| `--strict` | Escalate schema-drift warnings to hard rejects on newer-than-known tool versions |

Output per run:

```
claude-code: 47 files (43 skipped via watermark) — 124 records
             (118 skipped, 4 new prompts, 2 responses attached,
             0 standalone) — drift watch: 0 suspect files
```

### How the drift watchdog works

Each importer source declares:

- **`LastKnownVersion()`** — the highest tool version whose transcript
  shape this parser was validated against
- **`Anchor(record)`** — a per-source "this row looks healthy" predicate
  (today: `Role==Assistant && TokensIn>0`)

Per file, the driver counts anchor-eligible records (assistant rows
seen) vs anchor-passing records (assistant rows with token usage).
A file with the former but none of the latter triggers a warning. If
the observed tool version exceeds `LastKnownVersion`, it escalates:
`WARN(newer-version)`. `--strict` escalates further to `REJECT` and
skips stamping the mtime watermark so a fixed parser can re-walk on
next run.

Bump `LastKnownVersion` in the same commit that updates the parser
for a new transcript shape.

---

## Privacy (redact / purge)

### `ailog redact`

```sh
ailog redact 019da628-579c
```

Replaces `prompt`, `response`, and `notes` columns with the literal
string `"[redacted]"`. Metadata (id, project, branch, tokens, model,
timestamps) is preserved so you can see the entry existed. FTS index
re-syncs so the old text is no longer findable. Irreversible.

### `ailog purge`

```sh
ailog purge --before 2025-12-01 --yes
```

Hard-deletes every entry with `created_at < 2025-12-01` + its FTS
row. The `--yes` flag is required (no interactive prompt; shell
discipline).

### Auto-scrubbing at capture

Every prompt and response passes through a regex scrubber at write
time. Detected:

- AWS access keys (`AKIA...`)
- GitHub PATs (classic `ghp_`, fine-grained `github_pat_`)
- OpenAI tokens (`sk-...`)
- Anthropic tokens (`sk-ant-...`)
- Slack tokens (`xox[abpr]-...`)
- JWTs
- PEM private-key blocks

Override with `--no-redact` on `ailog add` for known-safe content.

---

## Export

```sh
ailog export --format json                       # every entry as NDJSON
ailog export --format md                         # every entry as markdown
ailog export --format md --project <p>           # filter first
ailog export --format md --session <id>          # one conversation thread
ailog export --format md --since 7d > week.md
```

`--format md` uses the shared renderer in `internal/render/markdown.go`
— same bytes as the web UI's `/session/{id}.md` download.

---

## Web UI

```sh
ailog ui                    # http://127.0.0.1:8090, auto-opens browser
ailog ui --port 9000        # explicit port; fails if taken
ailog ui --no-open          # don't launch browser
ailog ui --addr 0.0.0.0:8080 --allow-network   # LAN-accessible (no auth!)
```

When `--port` + `--addr` are both unset, ailog tries `127.0.0.1:8090`
first; if that's in use, asks the kernel for any free port
(`127.0.0.1:0`) and prints the chosen one. Explicit `--port` is
strict — it never silently falls through.

| Route | Description |
|---|---|
| `/` | Recent entries, newest first |
| `/search?q=…` | Live FTS (htmx swap, sub-100ms) |
| `/table` | 35-column datagrid with column chooser, per-column filters, sort, pagination, page-size selector |
| `/sessions` | Paginated session list with id/name/tool filter + 6 sort modes |
| `/session/{id}` | Threaded conversation, markdown-rendered |
| `/session/{id}.md` | Download the session as markdown (with YAML frontmatter) |
| `/entry/{id}` | Single-turn detail |
| `/entry/{id}.json` | Raw row as JSON |
| `/projects` | Project leaderboard |
| `/templates` | Starred entries |
| `/stats` | Tokens + counts + per-tool/model/project breakdowns; date-range picker (`?from=&to=`) |
| `/healthz` | Liveness (returns `ok`) |

Mutations via htmx:

- `POST /entry/{id}/star` — toggle star
- `POST /entry/{id}/tag` — edit tags
- `POST /entry/{id}/notes` — edit notes
- `POST /session/{id}/name` — rename session

Static assets (`static/app.css`, `static/htmx.min.js`, `static/table.js`)
are `//go:embed`'d into the binary. CSS/JS edits require a rebuild,
not just a server restart.

---

## Migrate / debug

```sh
ailog migrate                # apply pending migrations
ailog migrate --dry-run      # show DDL without running

ailog debug context          # print the resolved capture context as JSON
                             # (cwd, git, session, env, etc.)
```

The DB has a fast-path migration marker (`schema_meta` table) —
`Open()` skips running migrations when the stored version matches
the compile-time `SchemaVersion`. `ailog migrate` forces a full
check + re-apply regardless.

---

## Environment

| Variable | Meaning |
|---|---|
| `AILOG_HOME` | Override `~/.ailog` (where `log.db` lives). Useful for testing |
| `AILOG_SESSION_ID` | Pin the current session id. Normally set by harness hooks |
| `AILOG_TOOL` | Default `--tool` for `ailog add` when not supplied |
| `AILOG_PARENT_PID` | Override `pid` in captured context (defaults to `os.Getppid()`) |
| `AILOG_TERMINAL_TITLE` | Best-effort terminal-title stamp |
| `AILOG_HOOK_DEBUG` | Set to `0` to silence `~/.ailog/hook.log` (hook-invocation audit trail) |

---

## JSON field reference

Entries — common fields across `search --json`, `show --json`,
`last --json`, `/entry/{id}.json`:

| Field | Type | Source |
|---|---|---|
| `id` | string (UUID v7) | Auto at insert |
| `created_at` | RFC3339 | Live: insert time. Backfill: transcript timestamp |
| `tool` | string | `claude-code`, `codex`, `opencode`, etc. |
| `tool_version` | string | Per-tool native version string |
| `model` | string | `claude-opus-4-7`, `gpt-5-nano`, `gemma4`, etc. |
| `cwd` | string | Working directory at capture |
| `project` | string | Canonical `host/owner/repo`, or `basename(cwd)` fallback |
| `repo_owner` / `repo_name` / `repo_remote` | string | Parsed from git remote |
| `git_branch` / `git_commit` | string | At capture time |
| `session_id` / `session_name` | string | Per-tool native id + user-assigned label |
| `turn_index` / `parent_entry_id` | int / string | Auto-computed session chain |
| `prompt` / `response` | string | Scrubbed + FTS-indexed |
| `raw` | string | Provenance blob — shape depends on writer (hash / path / JSON — see schema comments) |
| `token_count_in` / `token_count_out` | int | Input / output tokens |
| `token_count_cache_read` / `token_count_cache_create` | int | Prompt cache hit / write |
| `stop_reason` | string | `end_turn` / `tool_use` / `max_tokens` / `stop_sequence` (claude-only today) |
| `permission_mode` | string | claude: `default`/`acceptEdits`/`bypassPermissions`/`plan`; codex: `on-request`/`never`/`unless-trusted` |
| `tags` | string (CSV) | User-applied |
| `starred` | bool | User flag |
| `notes` | string | User annotation, markdown |
| `hostname` / `user` / `shell` / `terminal` / `terminal_title` / `tty` / `pid` | string / int | Machine + shell context at capture |

Stats — emitted by `ailog stats --json`:

```json
{
  "total": 1234,
  "starred": 18,
  "distinct_sessions": 73,
  "first_entry_at": "2026-01-14T10:22:15Z",
  "last_entry_at":  "2026-04-21T17:43:02Z",
  "by_tool":    {"claude-code": 812, "codex": 340, "opencode": 82},
  "by_project": {"github.com/khanakia/ai-logger": 640, "…": 0},
  "by_model":   {"claude-opus-4-7": 512, "gpt-5.4": 340, "…": 0},
  "tokens_all_time": {"in": 127000, "out": 2300000, "cache_read": 1707300000, "cache_write": 5200000, "entries": 22751},
  "tokens_30d":      { /* same shape */ },
  "tokens_7d":       { /* same shape */ },
  "tokens_in_range": { /* populated when ?from= & ?to= are supplied */ },
  "range":           {"From": "…", "To": "…"},
  "tokens_by_tool":    { "claude-code": { /* TokenWindow */ }, "…": {} },
  "tokens_by_model":   { /* same */ },
  "tokens_by_project": { /* same */ }
}
```
