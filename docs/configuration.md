# Configuration

`baseliner` reads config from `baseliner.yaml` (or `--config PATH`).

## Schema

```yaml
scope:
  github:
    type: org
    name: my-org
    token_env: GITHUB_TOKEN
    include_archived: false # since v0.2.4
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
  The API root comes from the `GITHUB_API_URL` environment variable when it is
  set (added after v0.2.4), otherwise `https://api.github.com`. GitHub Actions
  sets it on every runner — on GitHub Enterprise Server, to that server's API.
- `scope.github.include_archived` (since v0.2.4; earlier releases reject
  the key): also scan archived repos (default `false`). Archived repos are
  skipped by default: they're read-only, so once one ages past `stale_repo`'s
  threshold it fails permanently and no commit can fix it. The number skipped
  is logged at info level.
- `scope.local.paths`: local directories to scan.
- `scope.include`: GitHub repo-name glob patterns to include.
- `scope.exclude`: GitHub repo-name glob patterns to exclude. Forks are
  discovered like any other repo; exclude them by name if you don't want them
  scanned.
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

Unknown keys are an error (exit `2`), in `baseliner.yaml` and in a custom
policy file alike — a misspelled key is reported with its line number rather
than silently ignored.

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
protects private (and `internal`) repos in those disclosure sinks, and in
everything else written to stderr: log lines (e.g. from `--open-issues`) and
messages such as the `--fail-under` list show a private repo's slug as
`private/redacted`. That redaction matches the full `org/repo` form, which is
how baseliner itself names repos.

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
- **The config file** is not covered. In a public control repo `baseliner.yaml`
  is itself public, so any private repo it names — a `repo_ignores` key, an
  `include`/`exclude` pattern — is disclosed there, and git history keeps it
  after a revert. Waivers are keyed by repo name, so today a private repo
  cannot be waived without naming it; see
  [#75](https://github.com/baselinerhq/baseliner/issues/75).

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
