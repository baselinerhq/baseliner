# Development

## Environment setup

```bash
uv sync --all-extras
```

## Quality checks

```bash
uv run ruff check .
uv run ruff format --check .
uv run pytest
```

## Local CLI smoke checks

```bash
uv run baseliner --version
uv run baseliner scan
```

## CI

CI is defined in `.github/workflows/ci.yml` and runs:

- lint (`ruff check`)
- format check (`ruff format --check`)
- tests (`pytest --cov=baseliner`)

## Dependency automation

- `.github/dependabot.yml` updates `uv` dependencies and GitHub Actions weekly.
- `.github/workflows/dependency-review.yml` blocks PRs that introduce high/critical-risk dependencies.

## Pre-commit

The repository includes `.pre-commit-config.yaml` with Ruff and basic hygiene hooks.

```bash
uvx pre-commit install
uvx pre-commit run --all-files
```
