# Security Policy

## Reporting a vulnerability

**Please do not report security vulnerabilities through public GitHub issues.**

Instead, report them privately via **[GitHub Security Advisories](https://github.com/thesatellite-ai/ai-logger-go/security/advisories/new)**.

Please include:

- A description of the vulnerability and its impact.
- Steps to reproduce (proof-of-concept if possible).
- Affected version(s) and environment.

We will acknowledge your report within a few days and keep you updated on remediation. We ask that you give us a reasonable window to release a fix before any public disclosure, and we're happy to credit you in the advisory.

## Scope notes

ailog stores all data locally (`~/.ailog/log.db`) and makes no network calls, so the most relevant areas are: the secret scrubber (ensuring credentials are redacted before they reach disk), file permissions (`0700` dir / `0600` db), and the local web UI bound to `127.0.0.1`.

## Supported versions

Security fixes are applied to the latest released version. Please upgrade to stay supported.

| Version | Supported |
|---|---|
| latest | ✅ |
| older | ❌ |
