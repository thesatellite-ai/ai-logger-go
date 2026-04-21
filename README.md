# ailog

Local, persistent log of AI prompts & responses. Captures every turn
automatically across Claude Code, OpenAI Codex, and opencode. Searches
across all your projects, all your time.

## Why you'd want this

If you use AI agents daily, you've already typed the same kind of
prompt dozens of times — *"why does this goroutine deadlock"*, *"write
a migration that backfills users.email"*, *"what's the idiomatic way to
bridge this API"*. Most of that work lives in transcripts you'll never
find again:

- **Between sessions.** Close the terminal and that clever prompt that
  unblocked you is gone. Next week you re-derive it from scratch.
- **Between tools.** A prompt you refined in Claude Code is invisible
  from Codex. No way to `grep "that thing I asked about X"` across
  both.
- **Between projects.** The debugging prompt that solved a race in
  `project-a` would solve the same race in `project-b`, but you
  re-type it from memory.
- **Between machines.** The laptop you were using last month has the
  answer; you're on a different one now.

`ailog` captures every prompt + response the moment it happens, tags
it with project, git branch, tool, session, model, token usage, and
timestamp, and keeps it **locally** in SQLite + FTS5 — no cloud, no
telemetry, no sync. One keyword and seven seconds later you're
reading what you wrote three months ago.

### What you get

- **Zero-friction capture.** Live hooks on Claude Code log every
  `UserPromptSubmit` + `Stop` event automatically. Codex / opencode
  get backfilled via `ailog import`.
- **Full-text search across everything.** One index, every tool,
  every project. Filter by `--project`, `--tool`, `--branch`,
  `--session`, `--since 7d` — any combination.
- **Rich per-turn metadata** — tokens in/out, cache hits, stop
  reason, git branch + commit, cwd, session graph. The stats page
  turns that into cost-per-tool / cost-per-project / cost-per-model
  breakdowns.
- **Curation primitives** — star keepers, tag by topic, name sessions
  retroactively, export one whole thread as markdown.
- **Built-in secret scrubber.** AWS keys, GitHub / OpenAI / Anthropic
  tokens, Slack webhooks, JWTs, PEM private keys — all redacted
  before they hit disk.
- **Claude Code skill** (installable via [skills.sh](https://skills.sh/)
  or `ailog skill install`) so the agent can recall your own history
  on demand: *"find that prompt where I fixed the btree index issue"*.
- **Web UI** at `http://127.0.0.1:8090` — browsable table, session
  threads, markdown-rendered detail, stats dashboard with date-range
  picker, markdown download per session.

### Deep dives

- **[docs/USE-CASES.md](./docs/USE-CASES.md)** — detailed scenarios,
  workflows, and the specific problems each feature solves
- **[docs/USAGE.md](./docs/USAGE.md)** — full CLI reference, every
  command, every flag, with examples

## Install

### macOS / Linux (one-liner)

```bash
curl -sL https://raw.githubusercontent.com/thesatellite-ai/ai-logger-go/main/install.sh | sh
```

Auto-detects your OS + architecture, downloads the latest release, installs to `/usr/local/bin/ailog`.

### Windows (PowerShell)

```powershell
irm https://raw.githubusercontent.com/thesatellite-ai/ai-logger-go/main/install.ps1 | iex
```

Installs to `%LOCALAPPDATA%\ailog` and adds it to your user PATH.

**Supported platforms:** macOS (Intel / Apple Silicon), Linux (amd64 / arm64), Windows (amd64 / arm64).

### Manual install

Grab a binary from the [Releases](https://github.com/thesatellite-ai/ai-logger-go/releases/latest) page and drop it in your `$PATH`.

### From source

Requires Go 1.22+. Uses [Taskfile](https://taskfile.dev/) — `brew install go-task` if missing.

```bash
git clone https://github.com/thesatellite-ai/ai-logger-go.git
cd ai-logger
task install                     # → $GOPATH/bin/ailog
```

## First-time setup

```bash
ailog init                                  # creates ~/.ailog/log.db
ailog hooks install --tool claude-code      # auto-log every Claude Code turn
ailog skill install                         # let Claude Code search your past prompts
```

`--tool` on `hooks install` is required (prevents silent "which tool
did I mean" confusion). Current adapters: `claude-code` (real),
`codex` / `opencode` (payload-adapter skeletons — backfill via
`ailog import` instead until those CLIs ship a hook schema).

Restart Claude Code. Done — every prompt and response now logs
automatically with project, git, and session context. Nothing to
remember in-session.

### Backfill from existing transcripts

If you already have Claude Code / Codex / opencode conversation history
on disk, import it into ailog in one shot:

```bash
ailog import claude-code    # walks ~/.claude/projects/**/*.jsonl
ailog import codex          # walks ~/.codex/sessions/**/*.jsonl
ailog import opencode       # walks ~/.local/share/opencode/storage
ailog import all            # all of the above
```

Per-line SHA dedup + per-file mtime watermarks make re-runs idempotent
and cheap. See [docs/USAGE.md](./docs/USAGE.md) for `--strict`,
`--force`, `--since` and the drift-detection mechanism.

### Skill install via [skills.sh](https://skills.sh/) (alternative)

If you already use [skills.sh](https://skills.sh/) to manage Claude Code skills, install ailog's skill from there:

```bash
npx skills add thesatellite-ai/ai-logger-go
```

This pulls `skills/ailog/SKILL.md` from this repo into `~/.claude/skills/ailog/`. Equivalent to running `ailog skill install`. Use whichever fits your workflow — you only need one.

## Daily use

```bash
ailog last 10                            # recent entries
ailog search "race condition" --since 7d
ailog search "auth" --project github.com/khanakia/api
ailog show 019da607-dfac                 # first 13 chars of id
ailog session show <session-id>          # all turns in one session
ailog star 019da607-dfac                 # mark as reusable template
ailog templates                          # list starred
ailog tag 019da607-dfac debugging,race
ailog stats                              # counts per tool / project / session
ailog export --format md --since 30d     # dump to markdown
ailog import                             # backfill from past Claude Code sessions
ailog redact <id>                        # scrub bodies, keep metadata
```

Every read command supports `--json` for scripting.

## Other AI tools

ailog isn't Claude-only. Set the tool name on the call:

```bash
ailog add --tool codex --session $ID --prompt "..."
ailog add --tool cursor --session $ID --prompt "..."
```

`--tool` and `--session` are both optional — defaults are sensible.

## Privacy

- 100% local. No network. No telemetry.
- `~/.ailog/` is 0700, `log.db` is 0600.
- Built-in secret scrubber catches AWS keys, GitHub/OpenAI/Anthropic tokens, Slack tokens, JWTs, private-key blocks before storage. Escape hatch: `ailog add --no-redact`.
- `ailog redact <id>` scrubs an entry after the fact. `ailog purge --before <date>` hard-deletes old history.

## Uninstall

### macOS / Linux

```bash
ailog hooks uninstall --tool claude-code   # first — surgically removes ailog entries from ~/.claude/settings.json (other hooks preserved)
sudo rm /usr/local/bin/ailog
rm -rf ~/.ailog
```

### Windows (PowerShell)

```powershell
Remove-Item "$env:LOCALAPPDATA\ailog" -Recurse -Force
Remove-Item "$env:USERPROFILE\.ailog" -Recurse -Force
```

You may also want to remove `%LOCALAPPDATA%\ailog` from your user PATH via System Settings.

## Build from source

```
task build           # dev build → ./bin/ailog
task build:release   # stripped, static
task build:all       # cross-compile darwin/linux × arm64/amd64
task test            # run all tests
task install         # → $GOPATH/bin/ailog
task version         # print version of the built binary
```

## Release

Tag and push:

```bash
git tag v0.1.0
git push origin v0.1.0
```

GitHub Actions runs GoReleaser, builds binaries for all 6 OS/arch targets, and publishes the release with checksums.
