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

## Docs

- [Getting Started](docs/getting-started.md)
- [Configuration](docs/configuration.md)
- [CLI Reference](docs/cli.md)
- [Development](docs/development.md)

## License

MIT. See [LICENSE](LICENSE).
