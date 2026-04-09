# Development

## Environment setup

```bash
uv sync --all-extras
```

## Quality checks

```bash
uv run ruff check .
uv run ruff format --check .
uv run pytest --cov=baseliner
```

## Pre-commit

```bash
uvx pre-commit install
uvx pre-commit run --all-files
```

## Local CLI smoke checks

```bash
uv run baseliner --version
uv run baseliner scan --help
```

## CI

CI is defined in `.github/workflows/ci.yml` and runs:

- lint (`ruff check`)
- format check (`ruff format --check`)
- tests (`pytest --cov=baseliner`)
- package build (`uv build`)
- wheel data check (`baseliner/policies/default.yaml` is present in built wheel)

## Dependency automation

- `.github/dependabot.yml` updates `uv` dependencies and GitHub Actions weekly.
- `.github/workflows/dependency-review.yml` blocks PRs that introduce high/critical-risk dependencies.
