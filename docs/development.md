# Development

## Requirements

- Go `1.25+`
- [`golangci-lint`](https://golangci-lint.run/) `v2` (for linting)

## Quality checks

```bash
go build ./...
go vet ./...
go test -race ./...
gofmt -l .            # should print nothing
golangci-lint run     # 0 issues
```

## Pre-commit

```bash
pre-commit install
pre-commit run --all-files
```

The hooks run `gofmt` and `go vet` (plus generic whitespace/YAML checks).

## Local CLI smoke checks

```bash
go run ./cmd/baseliner --version
go run ./cmd/baseliner scan --help
```

## Differential parity (vs the legacy Python tool)

`scripts/diff-acceptance.sh` runs the Go binary and a Python checkout against the same
config and diffs per-repo scores and verdicts. It backed the original Go migration; see
[VALIDATION.md](history/VALIDATION.md) and [SWEEP.md](history/SWEEP.md) for the evidence.

```bash
# needs a Python baseliner checkout + GITHUB_TOKEN for github-scope configs
./scripts/diff-acceptance.sh /path/to/baseliner.yaml /path/to/python-baseliner
```

## CI

CI is defined in `.github/workflows/ci.yml` and runs build, `go vet`, `go test -race`, and
`golangci-lint` on pushes to `main` and on pull requests. Tagging `vX.Y.Z` triggers
`.github/workflows/release.yml` (GoReleaser cross-compile + draft release).

## Dependency automation

- `.github/dependabot.yml` updates Go module dependencies and GitHub Actions weekly.
- `.github/workflows/dependency-review.yml` blocks PRs that introduce high/critical-risk dependencies.
