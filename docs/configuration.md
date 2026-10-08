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
  ignore_when: []
  # repo_waivers:
  #   allow: [ci_present]
privacy:
  # public_context: unset is off, except under GitHub Actions, where it is on
  private_repos: redact
```

## Fields

- `scope.github.type`: `org` or `user`.
- `scope.github.name`: org/user login used for discovery.
- `scope.github.token_env`: env var containing the GitHub token (default: `GITHUB_TOKEN`).
  The API root comes from the `GITHUB_API_URL` environment variable when it is
  set (since v0.2.5), otherwise `https://api.github.com`. GitHub Actions
  sets it on every runner — on GitHub Enterprise Server, to that server's API.
  It takes precedence, and Actions won't let `env:` override `GITHUB_*`
  variables — so to scan a github.com org from a GHES runner, run
  `unset GITHUB_API_URL` in the scan step's script before `baseliner scan`, or
  with the [GitHub Action](https://github.com/baselinerhq/baseliner-action) set
  `api-url: https://api.github.com`. Otherwise the token is sent to the GHES API.
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
- `policy.ignore_when`: rules that ignore checks on every repo of a visibility,
  without naming any repo. Each rule has `visibility` (any of `public`,
  `internal`, `private`) and `checks` (check IDs; an unknown one is a config
  error). A local repo has no visibility and matches no rule. See
  [Writing a custom policy](policies.md#ignoring-checks-per-deployment) for the
  values and how rules combine.
- `policy.repo_waivers.allow`: the checks a repo may waive for itself in a
  `.baseliner.yml` at its root. Unset, repo waivers do not apply. See
  [Repo waivers](policies.md#repo-waivers).
- `privacy.public_context`: set `true` when the scan output goes somewhere
  public (e.g. a public control repo's Actions logs and artifacts). Unset, it
  is off, except under GitHub Actions (`GITHUB_ACTIONS=true`), where it is on;
  set `false` there if the run's log and artifacts are private. An empty value
  (`public_context:` or `null`) counts as unset. The GitHub
  Action sets this from the control repo's visibility. See
  [Privacy guard](#privacy-guard).
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
`private/redacted` (in `redact` mode; `exclude` leaves those lines out, see the
table below). That redaction matches the full `org/repo` form in any
letter case, since GitHub names are case-insensitive: a log line can quote an
API URL that spells the org as GitHub does rather than as `scope.github.name`
does. A bare repo name without its org is not matched.

The guard activates only when the output is **public**. Set that with
`privacy.public_context: true`, the `--public-context` flag, or — most simply —
the [GitHub Action](control-repo.md#privacy-scanning-private-repos-from-a-public-control-repo),
which detects it automatically from the control repo's visibility. Under GitHub
Actions the CLI fails closed: when neither the flag nor `privacy.public_context`
is set, it treats the output as public and prints a line saying so, since a
workflow's log and artifacts are public whenever its repo is. Only an explicit
`false` turns the guard off there. When active, `privacy.private_repos` selects
the treatment:

| Mode | Behavior |
| --- | --- |
| `redact` (default) | Private/internal repos appear as `private/1`, `private/2`, … with their score and per-check pass/fail kept, but the real name and all finding messages stripped. Aggregate counts are unchanged. |
| `exclude` | Private/internal repos are dropped from the output entirely; aggregate counts cover only the disclosed repos. Log lines about them are omitted rather than masked, and the `--fail-under` / `--min-coverage` lists count them (`1 private repo(s)`) without a name or score. |
| `fail` | If any private/internal repo would be disclosed, baseliner writes nothing and exits `2` — forcing an explicit decision. |
| `allow` | No protection (today's behavior); discloses everything. |

What the guard does **not** change:

- **`--open-issues`** still opens/updates issues *inside* each scanned repo, so a
  private repo's findings issue stays in that private repo. Those are never a
  public sink and are left untouched.
- **The exit code** still reflects every repo: a private repo's failure (or a
  score below `--fail-under`) fails the run exactly as it would without the
  guard, and a findings issue that could not be searched for or written fails
  it with exit 2
  (see [CLI → Exit codes](cli.md#exit-codes)). Protection changes what is
  *disclosed*, never the pass/fail outcome.
- **The config file** is not covered. In a public control repo `baseliner.yaml`
  is itself public, so any private repo it names — a `repo_ignores` key, an
  `include`/`exclude` pattern — is disclosed there, and git history keeps it
  after a revert. To waive a check for private repos without naming one, use
  `policy.ignore_when` with `visibility: [private, internal]`. To waive one
  particular private repo, let it declare its own
  [repo waiver](policies.md#repo-waivers), which stays inside the repo.

`internal` repos (enterprise-visible) are protected like `private`, and so is
any GitHub visibility other than `public`. Local and non-GitHub repos have no
visibility signal and are always disclosed.

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
