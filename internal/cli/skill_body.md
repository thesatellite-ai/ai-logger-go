---
name: ailog
description: Local persistent log of AI prompts and responses. Use when the user asks to recall past prompts ("what did I ask about X", "find that prompt from last week"), curate entries ("save this as a template", "tag that"), or inspect history ("show last 5 prompts", "session stats"). Capture itself is automatic — do NOT call `ailog add` from this skill; hooks handle every turn.
---

# ailog — persistent conversation log

`ailog` is a local Go CLI (SQLite + FTS5) on PATH. It records every prompt + response in every Claude Code session **automatically** via `UserPromptSubmit` and `Stop` hooks installed in `~/.claude/settings.json`. Your job in this skill is **recall, search, and curation** — not capture.

## Never call these from this skill

- `ailog add …` — capture is handled by hooks. Calling `ailog add` would create a duplicate entry.
- `ailog init`, `ailog hooks install`, `ailog skill install` — one-time user setup. Don't run them unsolicited.

## When to invoke the CLI

### 1. User asks about past prompts

Trigger phrases: *"what did I ask about…", "find that prompt about…", "did I ever ask about…", "remember when I prompted about…"*.

```bash
ailog search "<keywords>" --json --limit 20
```

Read the JSON, summarize the matches for the user with short prompt previews + ids. If they want one specific entry:

```bash
ailog show <id-prefix>
```

First 13 chars of a UUID is usually enough to disambiguate.

### 2. Filter searches by project / session / tool / time

```bash
ailog search "race" --project github.com/khanakia/ai-logger --since 7d
ailog search "auth" --tool claude-code --branch feat/jwt
ailog search "migration" --session <uuid>
```

Flags: `--project / --tool / --session / --branch / --since (7d, 2w, 24h) / --limit / --json`.

### 3. User asks for recent history

```bash
ailog last 10              # last 10 entries
ailog session show         # all turns in a specific session (pass session id as arg)
```

### 4. Curation — "save / tag / label"

| User says | Run |
|---|---|
| "save that as a template" / "star that prompt" | `ailog star <id>` |
| "unstar that" | `ailog unstar <id>` |
| "show me my templates" / "what prompts have I saved" | `ailog templates` |
| "tag that with X, Y" | `ailog tag <id> X,Y` |
| "name this session 'debugging auth'" | `ailog session name "debugging auth"` |

### 5. Reports

| User says | Run |
|---|---|
| "how many prompts have I logged" / "stats" | `ailog stats` |
| "export this project's prompts to markdown" | `ailog export --format md --project <p>` |
| "dump everything as JSON" | `ailog export --format json` |

### 6. Sensitive content

| User says | Run |
|---|---|
| "redact entry X" / "scrub that prompt" | `ailog redact <id>` |
| "delete everything before <date>" | `ailog purge --before <date> --yes` (confirm with user first) |

Auto-scrubbing of common secret shapes (AWS/GH/OpenAI/Anthropic/Slack tokens, JWTs, private keys) happens on every capture — you don't need to manually scrub.

## Finding the right id

- `ailog last 5` shows 13-char id prefixes.
- `ailog search <term>` also prints prefixes.
- Pass the prefix to `show / redact / tag / star`; errors if ambiguous.

## Output to the user

When showing search results, prefer a short inline summary:

```
Found 3 prompts mentioning "race" (last 7 days):
1. 019da628-579c  2d ago, ai-logger, branch:main
   "fix the race condition in worker goroutine pool"
2. …
```

Don't paste raw JSON to the user. Use `--json` only for your own parsing.

## Fast reference

```
ailog last [N]              # recent entries
ailog search <q> [--json]   # FTS5 search, filters as above
ailog show <id>             # full entry
ailog session show [id]     # session turns
ailog session name "..."    # label a session
ailog tag <id> a,b          # add tags
ailog star <id>             # keeper
ailog unstar <id>           # remove star
ailog templates             # starred entries
ailog stats                 # counts
ailog export --format md|json
ailog redact <id>           # scrub bodies, keep metadata
ailog purge --before <date> --yes
```

Run `ailog --help` or `ailog hooks list` for the full surface.
