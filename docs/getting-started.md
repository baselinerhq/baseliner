# Getting Started

## Prerequisites

- Python `>=3.12`
- `uv`

## Setup

```bash
git clone <your-repo-url>
cd baseliner
uv sync --all-extras
```

## First scan (local repo)

Create `baseliner.yaml`:

```yaml
scope:
  local:
    paths:
      - .
policy:
  base: default
```

Run:

```bash
uv run baseliner scan --config baseliner.yaml --format table
```

## Scan GitHub repositories

Start from the provided example:

```bash
cp examples/baseliner.yaml baseliner.yaml
```

Edit `scope.github` (`type`, `name`) and then run:

```bash
export GITHUB_TOKEN=<your_pat>
uv run baseliner scan --config baseliner.yaml --format both --output-file results.json
```

Use `--open-issues` to create/update a `[baseliner]` findings issue in each GitHub repo.
