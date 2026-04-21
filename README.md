# ailog

Local, persistent log of AI prompts & responses. Captures every turn automatically. Search across all your projects, all your time.

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
ailog init                       # creates ~/.ailog/log.db
ailog hooks install              # automatic logging on every Claude Code turn
ailog skill install              # lets Claude search your past prompts
```

Restart Claude Code. Done — every prompt and response now logs automatically with project, git, and session context. Nothing to remember in-session.

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
sudo rm /usr/local/bin/ailog
rm -rf ~/.ailog
ailog hooks uninstall   # before deleting the binary, optional — removes hooks from settings.json
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
