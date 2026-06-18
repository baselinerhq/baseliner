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
privacy:
  public_context: false
  private_repos: redact
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
- `privacy.public_context`: set `true` when the scan output goes somewhere
  public (e.g. a public control repo's Actions logs and artifacts). Off by
  default. The GitHub Action sets this automatically from the control repo's
  visibility. See [Privacy guard](#privacy-guard).
- `privacy.private_repos`: how private/internal repos are treated when
  `public_context` is on — `redact` (default), `exclude`, `fail`, or `allow`.

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

## Privacy guard

When baseliner runs from a **public** control repo with a token that can read
**private** repos, the aggregate output would otherwise leak private repo names
and findings into public view — the console table appears in public Actions
logs, and `results.json` / SARIF are public artifacts. The privacy guard
protects private (and `internal`) repos in those disclosure sinks.

The guard activates only when the output is **public**. Set that with
`privacy.public_context: true`, the `--public-context` flag, or — most simply —
the [GitHub Action](control-repo.md#privacy-scanning-private-repos-from-a-public-control-repo),
which detects it automatically from the control repo's visibility. When active,
`privacy.private_repos` selects the treatment:

| Mode | Behavior |
| --- | --- |
| `redact` (default) | Private/internal repos appear as `private/1`, `private/2`, … with their score and per-check pass/fail kept, but the real name and all finding messages stripped. Aggregate counts are unchanged. |
| `exclude` | Private/internal repos are dropped from the output entirely; aggregate counts cover only the disclosed repos. |
| `fail` | If any private/internal repo would be disclosed, baseliner writes nothing and exits `2` — forcing an explicit decision. |
| `allow` | No protection (today's behavior); discloses everything. |

What the guard does **not** change:

- **`--open-issues`** still opens/updates issues *inside* each scanned repo, so a
  private repo's findings issue stays in that private repo. Those are never a
  public sink and are left untouched.
- **The exit code** still reflects every repo: a private repo's failure (or a
  score below `--fail-under`) fails the run exactly as it would without the
  guard. Protection changes what is *disclosed*, never the pass/fail outcome.

`internal` repos (enterprise-visible) are protected like `private`. Local and
non-GitHub repos have no visibility signal and are always disclosed.

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
