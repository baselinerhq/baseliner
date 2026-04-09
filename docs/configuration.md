# Configuration

`baseliner` reads config from `baseliner.yaml` (or `--config PATH`).

## Schema

```yaml
scope:
  github:
    type: org
    name: my-org
    token_env: GITHUB_TOKEN
  local:
    paths: []
  include: []
  exclude: []
policy:
  base: default
  ignore: []
  repo_ignores: {}
```

## Fields

- `scope.github.type`: `org` or `user`.
- `scope.github.name`: org/user login used for discovery.
- `scope.github.token_env`: env var containing the GitHub token (default: `GITHUB_TOKEN`).
- `scope.local.paths`: local directories to scan.
- `scope.include`: GitHub repo-name glob patterns to include.
- `scope.exclude`: GitHub repo-name glob patterns to exclude.
- `policy.base`: `default` or path to a custom policy YAML.
- `policy.ignore`: check IDs to ignore globally.
- `policy.repo_ignores`: check IDs to ignore per repo slug.

`include`/`exclude` apply to GitHub discovery only. Local paths are scanned as provided.

## Repo slug keys for `repo_ignores`

- GitHub repos use `scope.github.name/<repo-name>`.
- Local repos use the resolved absolute path string.

Example:

```yaml
policy:
  repo_ignores:
    my-org/legacy-service:
      - dependency_update_config
    /abs/path/to/local/repo:
      - stale_repo
```

## Minimal local-only config

```yaml
scope:
  local:
    paths:
      - .
policy:
  base: default
```

## Minimal GitHub-only config

```yaml
scope:
  github:
    type: org
    name: my-org
    token_env: GITHUB_TOKEN
policy:
  base: default
```
