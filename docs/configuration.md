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
  gitlab:
    group: my-group # full path; subgroups are included
    base_url: https://gitlab.com
    token_env: GITLAB_TOKEN
    include_archived: false
  gitea: # Gitea or Forgejo, such as Codeberg
    type: org
    name: my-org
    base_url: https://codeberg.org
    token_env: GITEA_TOKEN
    include_archived: false
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
- `scope.gitlab.group`: the GitLab group's full path, such as `my-group` or
  `my-group/platform`. Projects in its subgroups are scanned too; projects
  shared into it from other groups are not. See [GitLab](#gitlab).
- `scope.gitlab.base_url`: the instance's root URL (default
  `https://gitlab.com`); an `/api/v4` suffix is accepted and dropped. The
  token is sent only there: a redirect or next-page link elsewhere is not
  followed. Use `https`; with `http` the token travels unencrypted. It is
  never taken from the environment.
- `scope.gitlab.token_env`: env var containing the GitLab token (default:
  `GITLAB_TOKEN`). See [Token permissions](token-permissions.md#gitlab).
- `scope.gitlab.include_archived`: also scan archived projects (default
  `false`), as for GitHub.
- `scope.gitea.type`, `scope.gitea.name`: an organisation (`org`) or a user
  (`user`) on a Gitea or Forgejo instance. See [Gitea and Forgejo](#gitea-and-forgejo).
- `scope.gitea.base_url`: the instance's root URL (default
  `https://codeberg.org`); an `/api/v1` suffix is accepted and dropped. The
  token is sent only there.
- `scope.gitea.token_env`: env var containing the token (default:
  `GITEA_TOKEN`). See [Token permissions](token-permissions.md#gitea-and-forgejo).
- `scope.gitea.include_archived`: also scan archived repos (default `false`).
- `scope.local.paths`: local directories to scan.
- `scope.include`: glob patterns of repos to include: on GitHub the repo
  name, on GitLab the project's path relative to the group (`team/*`).
- `scope.exclude`: glob patterns of repos to exclude, matched the same way.
  Forks are discovered like any other repo; exclude them by name if you
  don't want them scanned.
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

- GitHub repos use `scope.github.name/<repo-name>`. With `type: user`, a
  listed repo owned by another login (an organisation the user belongs to,
  or a collaboration) uses that owner's login: `<owner>/<repo-name>`.
- Gitea and Forgejo repos use `scope.gitea.name/<repo-name>`, as GitHub
  does (a user scope's repo owned by another login: `<owner>/<repo-name>`).
- GitLab projects use the project's full path as GitLab spells it
  (`path_with_namespace`), such as `my-group/team/service`.
- Local repos use the resolved absolute path string.

A key applies to every repo with that slug. When the same slug exists on
two forges in one scan, prefix the forge to target one of them:
`github:`, `gitlab:`, `gitea:` or `local:`, as in `gitlab:my-group/service`.
The two keys combine.

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

## GitLab

`scope.gitlab` scans a GitLab group read-only, on gitlab.com or a
self-managed instance, beside or instead of GitHub and local paths. The same
checks run, with these differences:

- **Privacy.** GitLab's `internal` (any signed-in user of the instance) and
  `private` projects are protected like GitHub's. A project whose visibility
  GitLab does not report counts as private. Nested paths
  (`group/sub/project`), the config's spelling of them, and their
  `%2F`-encoded form (`group%2Fsub%2Fproject`) are redacted wherever they
  appear. baseliner's own GitLab requests and errors name projects by
  numeric ID, never by path.
- **Platform checks** (branch protection and rulesets) report `unknown`:
  they read GitHub's API only for now.
- **A repository the token cannot read**, which GitLab reports by leaving
  out the default branch, is read as unread, not empty: its file checks
  report `unknown`.
- **`ci_present`** counts a `.gitlab-ci.yml` at the root, by file presence,
  or the project's custom CI configuration path when one is set: a file in
  the repo when it exists, or a configuration in another project or at a URL.
  When GitLab does not report the setting to the token (it reports none to
  an anonymous request), the default `.gitlab-ci.yml` is assumed.
- **`codeowners_exists`** also accepts `.gitlab/CODEOWNERS`, where GitLab
  reads it.
- **`stale_repo`** uses the project's last activity, which GitLab also
  moves on issue and merge request activity, so it is more lenient than
  GitHub's last push.
- **`--open-issues`** delivers findings issues to GitHub repos only. With a
  GitLab-only scope it exits `2`; in a mixed scope GitLab projects are
  skipped, with a count, including a GitLab project that shares a GitHub
  repo's path.
- A repo with the same path on two forges, such as GitHub and GitLab or
  GitHub and Gitea, appears twice in the output under that slug, once per
  forge, and each result's `forge` field says which. For the privacy guard,
  the slug's visibility is the more protective of the two, and a masked
  repo's `forge` is left out.

```yaml
scope:
  gitlab:
    group: my-group
    base_url: https://gitlab.example.com
policy:
  base: default
```

## Gitea and Forgejo

`scope.gitea` scans an organisation's or a user's repos on a Gitea or
Forgejo instance, such as Codeberg, read-only. The same checks run, with
these differences:

- **Privacy.** A repo is only as visible as its owner: a repo that is
  public by its own setting but belongs to a `limited` organisation (visible
  to signed-in users) counts as `internal`, and one in a `private`
  organisation as `private`. Forgejo reports a public repo of a limited
  organisation as neither private nor internal, so going by the repo's own
  flags alone would show it as public.
  On an instance that requires sign-in to view anything
  (`REQUIRE_SIGNIN_VIEW`), the API still calls public repos public. So
  baseliner reads one of them without the token first. If that fails, every
  public repo there counts as `internal`, and one log line says so.
- **Platform checks** report `unknown`.
- **`ci_present`** also counts Gitea and Forgejo Actions workflows
  (`.gitea/workflows/`, `.forgejo/workflows/`) and Woodpecker CI
  (`.woodpecker.yml`, `.woodpecker/`); **`codeowners_exists`** also accepts
  `.gitea/CODEOWNERS` and `.forgejo/CODEOWNERS`.
- **`stale_repo`** uses the latest commit on the default branch.
- **`--open-issues`** is GitHub only, as for GitLab (#142).

```yaml
scope:
  gitea:
    type: org
    name: my-org          # on Codeberg by default
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
