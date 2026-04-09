# baseliner

`baseliner` is a Python CLI that scans repositories for baseline compliance and reports findings as JSON and/or a console summary.

## Current capabilities

- Discovers repositories from local paths and/or GitHub org/user scope.
- Collects filesystem and git metadata into a normalized repository model.
- Evaluates a built-in default policy with 10 checks.
- Outputs results as JSON, table, or both.
- Optionally opens or updates a per-repo GitHub findings issue (`--open-issues`).

## Requirements

- Python `>=3.12`
- [`uv`](https://docs.astral.sh/uv/)

GitHub scanning and `--open-issues` require a GitHub token in your configured env var (default: `GITHUB_TOKEN`).

## Install (from source)

```bash
git clone <your-repo-url>
cd baseliner
uv sync --all-extras
```

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
uv run baseliner scan --config baseliner.yaml --format table
```

## Quick start (GitHub)

Use the example config and edit it for your org/user:

```bash
cp examples/baseliner.yaml baseliner.yaml
```

Then run:

```bash
export GITHUB_TOKEN=<your_pat>
uv run baseliner scan --config baseliner.yaml --format both --output-file results.json
```

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
   curl -fsSL https://raw.githubusercontent.com/CameronBrooks11/baseliner/main/examples/control-repo-workflow.yml \
     -o .github/workflows/baseliner.yml
   ```
2. Copy and edit the config:
   ```bash
   curl -fsSL https://raw.githubusercontent.com/CameronBrooks11/baseliner/main/examples/baseliner.yaml \
     -o baseliner.yaml
   ```
3. Add repo secret `BASELINER_TOKEN` in GitHub Actions settings.
4. Trigger `workflow_dispatch` and confirm `results.json` uploads as an artifact.

## Docs

- [Getting Started](docs/getting-started.md)
- [Configuration](docs/configuration.md)
- [CLI Reference](docs/cli.md)
- [Development](docs/development.md)
- [Control Repo](docs/control-repo.md)

## License

MIT. See [LICENSE](LICENSE).
