# Contributing to baseliner

Thanks for your interest in improving baseliner! This guide covers how to build,
test, and submit changes.

## Prerequisites

- Go `1.25+`
- [`golangci-lint`](https://golangci-lint.run/) `v2` (for linting)
- Optionally [`pre-commit`](https://pre-commit.com/) for the local hooks

## Build, test, lint

```bash
go build ./...
go vet ./...
go test -race ./...
gofmt -l .          # must print nothing
golangci-lint run   # must report 0 issues
```

All of these run in CI on every pull request (`.github/workflows/ci.yml`).

## Project layout

- `cmd/baseliner` — CLI entrypoint (cobra)
- `internal/` — the engine: `models`, `policy`, `checks`, `collectors`,
  `discovery`, `engine`, `output`, `actions`, `runner`, `config`
- `docs/` — user docs; `docs/history/` — migration-era records
- `examples/` — sample config + control-repo workflow

See [docs/development.md](docs/development.md) for more detail.

## Pull requests

1. Branch off `main` (e.g. `feat/...`, `fix/...`, `docs/...`).
2. Keep changes focused; add tests for behavior changes.
3. Ensure the full check suite above passes locally.
4. Open a PR; fill in the template. CI must be green before merge.

### Commit style

Use [Conventional Commits](https://www.conventionalcommits.org/): `type(scope): description`

- Types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `ci`, `style`
- Imperative mood, lowercase, no trailing period
- Body lines wrapped at ~72 chars

Commit messages drive the grouped release changelog, so accurate types matter.

## Behavioral parity

baseliner has a precise output contract (scores, verdicts, JSON shape, exit
codes). If you change scoring or output, update the relevant golden tests and
note the change. The original Go↔Python parity work is archived under
[docs/history/](docs/history/).

## Reporting bugs and requesting features

Use the issue templates. For security issues, **do not** open a public issue —
see [SECURITY.md](SECURITY.md).
