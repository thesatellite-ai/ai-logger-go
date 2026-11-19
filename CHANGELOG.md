# Changelog

All notable changes to **ailog** are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Gemini CLI and pi are captured: `ailog import gemini`, `ailog import pi`, and `ailog hooks install --tool gemini` (BeforeAgent / AfterAgent hooks).
- `ailog hooks install --tool codex` writes Codex's `hooks.json` (real Codex payloads are decoded); install prints the steps Codex needs that ailog does not take.
- `ailog import opencode` reads `opencode.db` (opencode 1.2+) as well as the legacy `storage/` tree.
- Import and hooks cover every home an agent has (`~/.claude` and every `~/.claude-*`, every Codex home); `--from` / `--home` narrow to one.
- Imports read a grown transcript from where the last run stopped (`import_state.read_offset`, added to an existing `log.db` on open); the Stop hook reads only what the turn appended.
### Changed
- Every agent's homes, transcripts, hook payloads and hook installation now come from [agentio](https://github.com/ubgo/agentio); ailog's own transcript parsers and settings editor are gone. Rows already in `log.db` are recognised by the same line-hash keys (no duplicates); new rows also get message-id keys.
- Settings backups are named `<file>.agentio-bak-<timestamp>` (was `<file>.bak-<timestamp>`). `hooks install --dry-run` prints the plan (replace / present / add) instead of a line diff.
- `hooks install` replaces an ailog entry that runs another ailog binary with one running this binary, instead of adding a second entry.
- `import --from` names an agent home (`~/.claude-work`); an older root such as `~/.claude/projects` still works.
- An undecodable transcript line is skipped with a warning instead of ending the read of that file.
- Codex commentary messages are never stored as answers; an answer whose token count is not yet written is stored on the next run, with its usage.
### Deprecated
### Removed
- `ailog hook opencode` (a guessed payload; opencode runs no shell-command hooks; use `ailog import opencode`).
### Fixed
- `ailog hooks install --tool claude-code` merges into `settings.json` instead of replacing every `UserPromptSubmit` / `Stop` hook; it is idempotent, keeps other keys and formatting byte for byte, backs the file up to `settings.json.bak-<timestamp>` and has `--dry-run`. `hooks uninstall` removes only ailog's own commands.
- `ailog import codex` reads current Codex rollouts (0.158+, `item_completed` items) as well as the old `user_message` / `agent_message` events.

### Security
- `~/.ailog/hook.log` is off by default (enable with `AILOG_HOOK_DEBUG=1`); payloads are redacted before writing and the file rotates at 10 MiB.

<!--
Release process:
  1. Move the relevant [Unreleased] entries under a new version heading below.
  2. Date it: ## [1.2.0] - YYYY-MM-DD
  3. Tag the release (e.g. v1.2.0) and update the link refs at the bottom.
-->

[Unreleased]: https://github.com/thesatellite-ai/ai-logger-go/commits/main
