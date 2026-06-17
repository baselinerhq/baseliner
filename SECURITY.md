# Security Policy

## Supported versions

baseliner is pre-1.0. Security fixes are applied to the latest released minor
version.

| Version | Supported |
|---------|-----------|
| 0.1.x   | ✅        |
| < 0.1   | ❌        |

## Reporting a vulnerability

**Please do not report security vulnerabilities through public GitHub issues.**

Use GitHub's private vulnerability reporting:

1. Go to the [Security tab](https://github.com/baselinerhq/baseliner/security)
   of the repository.
2. Click **Report a vulnerability** and provide:
   - a description and impact assessment,
   - steps to reproduce (a minimal config/repo if possible),
   - the baseliner version (`baseliner --version`) and OS/arch.

You can expect an acknowledgement within a few days. We'll keep you informed as
we work on a fix and coordinate a disclosure timeline.

## Scope

baseliner reads repository metadata and (optionally) calls the GitHub API with a
token you provide. Reports of particular interest:

- token/credential handling (leakage in logs, output, or issues),
- unsafe filesystem traversal during local scans,
- issues in the release/distribution pipeline (binaries, install script, tap).
