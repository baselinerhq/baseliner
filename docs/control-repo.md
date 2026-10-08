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
  - Issues: Write (only needed for `--open-issues`)
  - Actions: Read (optional: lets `ci_present` see workflows GitHub has
    disabled or, on a fork, never enabled; without it the check falls back
    to file presence and passes them, with one warning in the run log)

Token scope must include every repository you plan to scan.

If your org uses SAML SSO, authorize the token for the org after creation.

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
| A repo's evidence could not be fully read (except workflow state: `ci_present` falls back to file presence) | `1` | red |
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
A public control repo is exposed to this whatever the state of the fleet,
because the scan itself never commits to the control repo: it only reads, and
`--open-issues` writes into the scanned repos. When the schedule stops, no run
fails. Scan runs do not count as activity: baselinerhq's own control repo ran
successfully every week, then stopped running about 60 days after its last
commit, which was also its last activity of any kind.

Check for it:

```bash
gh api repos/<owner>/<control-repo>/actions/workflows \
  --jq '.workflows[] | "\(.path) \(.state)"'
```

Anything other than `active` means the scan is not running. The inactivity case
shows `disabled_inactivity`; `disabled_manually` and `disabled_fork` stop it
too. Re-enable it with `gh workflow enable baseliner.yml -R <owner>/<control-repo>`.

GitHub does not define "repository activity". To stay ahead of it:

- **Keep the control repo private.** The documented rule is for public
  repositories.
- **Commit to it at least every 60 days.** Routine dependency updates are not
  enough on their own: with actions pinned to major tags (`@v4`), Dependabot
  opens a PR only when a new major version ships, which can be months apart.
- **Run the check above on a schedule of your own,** somewhere that sees
  regular commits, and alert on any state other than `active`.

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

Outside the Action (raw CLI), signal a public context explicitly with
`--public-context` or `privacy.public_context: true`; the
[workflow template](../examples/control-repo-workflow.yml) passes
`--public-context`. Full details and the mode
table are in [Configuration → Privacy guard](configuration.md#privacy-guard).

The guard covers the scan's output, not your config: a public control repo's
`baseliner.yaml` is public, so don't name a private repo in it (`repo_ignores`,
`include`/`exclude`). That currently means a private repo can't be given a
per-repo waiver from a public control repo — tracked in
[#75](https://github.com/baselinerhq/baseliner/issues/75).

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
   private repos out of the run's public log and `results.json`. Leave it in
   unless the control repo is private; see
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
