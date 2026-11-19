# Changelog

All notable changes to **ailog** are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
### Changed
### Deprecated
### Removed
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
