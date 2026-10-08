# Control Repo Guide

Use a dedicated control repo to run `baseliner` on a schedule.

## What the control repo contains

- `.github/workflows/baseliner.yml` (from `examples/control-repo-workflow.yml`)
- `baseliner.yaml` (from `examples/baseliner.yaml`)
- One secret: `BASELINER_TOKEN`

## Token requirements

Use either:

- A classic PAT with `repo` scope, or
- A fine-grained PAT with repository permissions:
  - Metadata: Read
  - Contents: Read
  - Issues: Read and write (only needed for `--open-issues`)
  - Actions: Read (optional: lets `ci_present` see workflows GitHub has
    disabled or, on a fork, never enabled; without it the check falls back
    to file presence and passes them, with one warning in the run log)
  - Administration: Read (only for the platform checks, to read classic
    branch protection)

[Token permissions](token-permissions.md) lists what each feature calls and
what it reports without the permission.

Token scope must include every repository you plan to scan.

If your org uses SAML SSO, authorize a classic token for the org after creation;
a fine-grained token is authorized when created, with the org as resource owner.

## Using the GitHub Action

The simplest workflow uses [`baselinerhq/baseliner-action`](https://github.com/baselinerhq/baseliner-action)
— no install boilerplate:

```yaml
jobs:
  scan:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: baselinerhq/baseliner-action@v1
        with:
          config: baseliner.yaml
          output-file: results.json
          open-issues: "true"
          fail-under: "0"
          extra-args: --min-coverage 1.0
        env:
          GITHUB_TOKEN: ${{ secrets.BASELINER_TOKEN }}
      - uses: actions/upload-artifact@v4
        if: always()
        with:
          name: baseliner-results
          path: results.json
```

Inputs map to the CLI flags (`format`, `sarif-file`, `fail-under`, `open-issues`,
…). Or use the install-script template below.

## Monitor mode: what a red run means

A control repo is a monitor, not a gate. With `--open-issues`, each repo's
findings are delivered as a findings issue inside that repo. Failing the control
run on the same findings repeats the alert in a place most repo owners never
look, and a run that is red every week stops being read.

Both templates therefore run with `--fail-under 0 --min-coverage 1.0`:

| Situation | Exit | Run |
| --- | --- | --- |
| Findings, every repo assessed | `0` | green; findings are in each GitHub repo's issue (archived repos and repos with Issues disabled are skipped) |
| Evidence a check's result depends on could not be read, so the check is `unknown` (except workflow state: `ci_present` falls back to file presence). An unread directory that no failing check looks in leaves the run green | `1` | red |
| Runtime, config or auth error, or a findings issue that could not be searched for or written | `2` | red |

A `2` outranks a `1`; when both happen, both are printed. With `--open-issues`
the token needs Issues read and write on every scanned repo; repos that are
archived or have Issues disabled are skipped.

So a red run means the scan, or the delivery of its findings, broke. To gate on findings instead, for
example in a single repo's own CI, drop `--fail-under 0` and the default
per-check gate applies (see [CLI → Exit codes](cli.md#exit-codes)).

## Keeping the schedule alive

GitHub's documentation: *"In a public repository, scheduled workflows are
automatically disabled when no repository activity has occurred in 60 days."*
A control repo is exposed to this whatever the state of the fleet: the scan
only reads, and `--open-issues` writes into the scanned repos. Neither counts as
activity. Scheduled runs don't, and neither do issues, even opened daily.
baselinerhq's own control repo ran every week and then stopped, 60 days after its
last commit. When the schedule stops, no run fails.

**The template keeps itself alive.** Its `keepalive` job calls GitHub's
[enable-workflow API](https://docs.github.com/en/rest/actions/workflows#enable-a-workflow)
on its own workflow each run, with `permissions: actions: write` on the default
token (no personal token, no commits):

```yaml
  keepalive:
    runs-on: ubuntu-latest
    permissions:
      actions: write
    steps:
      - run: gh api -X PUT "repos/$GITHUB_REPOSITORY/actions/workflows/baseliner.yml/enable"
        env:
          GH_TOKEN: ${{ github.token }}
```

GitHub does not document that this restarts the 60-day clock. The evidence that
it does is that public repos using it, idle for over a year, still run on
schedule, while a workflow without it in the same repo was disabled. If you
use [baseliner-action](#using-the-github-action), add the same job to your
workflow, and change `baseliner.yml` if your workflow file is named otherwise.

The keepalive cannot revive a workflow that is already disabled, because the
job no longer runs. Check for that:

```bash
gh api repos/<owner>/<control-repo>/actions/workflows \
  --jq '.workflows[] | "\(.path) \(.state)"'
```

Anything other than `active` means the scan is not running. The inactivity case
shows `disabled_inactivity`; `disabled_manually` and `disabled_fork` stop it
too. Re-enable it with `gh workflow enable baseliner.yml -R <owner>/<control-repo>`,
or by pushing an edit to the workflow file. GitHub documents that a commit
changing the `cron` schedule reactivates it, and baselinerhq's control repo was
reactivated by a push that changed another line of the file. (`gh workflow
enable` on a workflow that is already active reports `could not find any
workflows named …`; that means there was nothing to enable.)

A push to any branch also appears to count as activity, and a private control
repo is outside the documented rule, which names public repositories. Neither
needs the keepalive, but neither is guaranteed either.

GitHub's 60 days is the only such rule baseliner knows of: GitLab, Forgejo,
Gitea and Bitbucket document no inactivity cut-off for scheduled pipelines.
(Codeberg runs Forgejo; no Codeberg-specific policy was found either way.)

## Privacy: scanning private repos from a public control repo

If your control repo is **public** but its token can read **private** repos, the
aggregate output would leak private repo names and findings into public view —
the console table is in the public Actions log, and `results.json` / SARIF are
public artifacts. (Per-repo `--open-issues` issues are *not* a leak: they open
inside each scanned repo, so a private repo's issue stays private.)

The [`baseliner-action`](https://github.com/baselinerhq/baseliner-action) handles
this for you: it looks up the control repo's visibility through the GitHub API
and enables the privacy guard automatically unless the repo is definitively
private or internal — if the visibility can't be read, it fails closed and
enables the guard. Private/internal repos are **redacted** by default
(shown as `private/1` with score but no name or finding detail). No
configuration needed.

To choose a different treatment, set it in `baseliner.yaml`:

```yaml
privacy:
  private_repos: exclude   # redact (default) | exclude | fail | allow
```

Outside the Action (raw CLI), the guard is on under GitHub Actions unless you
turn it off with `--public-context=false` or `privacy.public_context: false`, and
baseliner prints a line saying it inferred it. Pass `--public-context` (or set
`privacy.public_context: true`) to make it explicit; the
[workflow template](../examples/control-repo-workflow.yml) does. Full details and the mode
table are in [Configuration → Privacy guard](configuration.md#privacy-guard).

The guard covers the scan's output, not your config: a public control repo's
`baseliner.yaml` is public, so don't name a private repo in it (`repo_ignores`,
`include`/`exclude`). To waive a check for all private repos without naming any,
use [`policy.ignore_when`](policies.md#ignoring-checks-per-deployment). A waiver
for one particular private repo still needs its name, so it can't be given from
a public control repo yet; see
[#103](https://github.com/baselinerhq/baseliner/issues/103).

## Setup checklist

1. Create or choose a control repo.
2. Copy the workflow template:
   ```bash
   mkdir -p .github/workflows
   curl -fsSL https://raw.githubusercontent.com/baselinerhq/baseliner/main/examples/control-repo-workflow.yml \
     -o .github/workflows/baseliner.yml
   ```
3. Copy config and edit scope filters:
   ```bash
   curl -fsSL https://raw.githubusercontent.com/baselinerhq/baseliner/main/examples/baseliner.yaml \
     -o baseliner.yaml
   ```
4. Add secret `BASELINER_TOKEN` in Settings -> Secrets and variables -> Actions.
5. Check the privacy guard. The template passes `--public-context`, which keeps
   private repos out of the run's public log and `results.json`. If the control
   repo is private, change it to `--public-context=false`; leaving it out still
   turns the guard on under Actions. See
   [Privacy](#privacy-scanning-private-repos-from-a-public-control-repo).
6. Trigger `workflow_dispatch`.
7. Confirm:
   - Workflow run completes.
   - `results.json` uploads as artifact.
   - When `--open-issues` is enabled, findings issue is created/updated.

## Manual smoke test (local)

Run against a non-production account or org first.

```bash
export GITHUB_TOKEN=<your_pat>
baseliner scan \
  --config examples/baseliner.yaml \
  --output-file /tmp/results.json \
  --open-issues \
  --format both \
  --verbose
```

Validate:

- Discovery count matches expected repos.
- Include/exclude filters behave as expected.
- `/tmp/results.json` exists and parses.
- Re-run updates existing findings issue (no duplicates).
- `--dry-run` performs no write actions.
