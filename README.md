# baseliner

[![CI](https://github.com/baselinerhq/baseliner/actions/workflows/ci.yml/badge.svg)](https://github.com/baselinerhq/baseliner/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/baselinerhq/baseliner)](https://github.com/baselinerhq/baseliner/releases/latest)
[![Go Reference](https://pkg.go.dev/badge/github.com/baselinerhq/baseliner.svg)](https://pkg.go.dev/github.com/baselinerhq/baseliner)
[![Go Report Card](https://goreportcard.com/badge/github.com/baselinerhq/baseliner)](https://goreportcard.com/report/github.com/baselinerhq/baseliner)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

`baseliner` is a single-binary CLI that scans a fleet of repositories for baseline
compliance and reports findings as JSON and/or a console summary. It runs against local
git checkouts and/or GitHub org/user scopes, and can optionally open a findings issue per
repository.

## Current capabilities

- Discovers repositories from local paths and/or GitHub org/user scope.
- Collects filesystem and git metadata into a normalized repository model.
- Evaluates a built-in default policy with 10 checks.
- Outputs results as JSON, table, or both.
- Optionally opens/updates a GitHub findings issue on repos that have findings, and closes it when a repo becomes compliant (`--open-issues`).

## Requirements

- None for the prebuilt binary — it is statically linked, no runtime needed.
- GitHub scanning and `--open-issues` require a GitHub token in your configured env var
  (default: `GITHUB_TOKEN`).

## Install

```bash
# Install script (Linux/macOS) — installs to ~/.local/bin:
curl -fsSL https://raw.githubusercontent.com/baselinerhq/baseliner/main/scripts/install.sh | bash

# Or with the Go toolchain:
go install github.com/baselinerhq/baseliner/cmd/baseliner@latest
```

Prebuilt archives for Linux/macOS/Windows (amd64/arm64) are on the
[releases page](https://github.com/baselinerhq/baseliner/releases). See
[docs/install.md](docs/install.md) for all options (including the planned Homebrew tap).

## Quick start (local)

Create a minimal `baseliner.yaml`:

```yaml
scope:
  local:
    paths:
      - .
policy:
  base: default
```

Run a scan:

```bash
baseliner scan --config baseliner.yaml --format table
```

## Quick start (GitHub)

Use the example config and edit it for your org/user:

```bash
cp examples/baseliner.yaml baseliner.yaml
export GITHUB_TOKEN=<your_pat>
baseliner scan --config baseliner.yaml --format both --output-file results.json
```

### Scan flags

| Flag | Default | Description |
|---|---|---|
| `--config PATH` | `baseliner.yaml` | Path to config file |
| `--output-file PATH` | unset | Write JSON results to file |
| `--sarif-file PATH` | unset | Also write SARIF 2.1.0 (for GitHub code scanning) |
| `--format` | `both` | `json`, `table`, or `both` |
| `--open-issues` | off | Open/update a findings issue on repos with findings; close it when a repo is compliant |
| `--fail-under` | unset | Exit 1 if any repo scores below this threshold (0.0–1.0); replaces the default per-check gate |
| `--dry-run` | off | Skip API write calls |
| `--verbose` | off | Debug logging |
| `--quiet` | off | Suppress table output; keep errors |

### Exit codes

| Code | Meaning |
|---|---|
| `0` | Scan completed and all repos passed (or, with `--fail-under`, all repos met the threshold) |
| `1` | Scan completed with one or more failing repos (or, with `--fail-under`, below the threshold) |
| `2` | Runtime/config/auth/discovery error |

With `--fail-under X`, the exit-1 gate becomes "any repo scored below `X`" — so a
repo with a failing check still passes as long as its score is `>= X`. This is for
gradual rollout: tolerate sub-perfect repos above a bar.

## Control Repo Setup

`baseliner` is designed to run from a dedicated control repo that owns the
scan config and scheduled workflow.

### Prerequisites

- A GitHub token with access to all target repos.
- If using a fine-grained token, grant:

  | Permission | Level | Why |
  |---|---|---|
  | Metadata | Read | Org/user repo discovery |
  | Contents | Read | File checks via Contents API |
  | Issues | Write | Required only when using `--open-issues` |

- For orgs with SAML SSO, authorize the token for that org after creating it.

### Steps

1. In your control repo, copy the workflow template:
   ```bash
   mkdir -p .github/workflows
   curl -fsSL https://raw.githubusercontent.com/baselinerhq/baseliner/main/examples/control-repo-workflow.yml \
     -o .github/workflows/baseliner.yml
   ```
2. Copy and edit the config:
   ```bash
   curl -fsSL https://raw.githubusercontent.com/baselinerhq/baseliner/main/examples/baseliner.yaml \
     -o baseliner.yaml
   ```
3. Add repo secret `BASELINER_TOKEN` in GitHub Actions settings.
4. Trigger `workflow_dispatch` and confirm `results.json` uploads as an artifact.

## Docs

- [Getting Started](docs/getting-started.md)
- [Install](docs/install.md)
- [Configuration](docs/configuration.md)
- [Writing a custom policy](docs/policies.md)
- [CLI Reference](docs/cli.md)
- [Development](docs/development.md)
- [Control Repo](docs/control-repo.md)
- [Roadmap](docs/ROADMAP.md)
- [Project history](docs/history/) — the Python → Go migration record

## License

MIT. See [LICENSE](LICENSE).
