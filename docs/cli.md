# CLI Reference

## Root command

```bash
baseliner --help
```

Global options:

- `--version` show version and exit
- `--help` show help

Commands: [`scan`](#scan) · [`checks`](#checks) · [`policy`](#policy) · [`completion`](#completion)

## scan

```bash
baseliner scan --help
```

Options:

- `--config PATH` path to config file (default: `baseliner.yaml`)
- `--output-file PATH` write JSON output to a file
- `--sarif-file PATH` also write SARIF 2.1.0 to a file (for GitHub code scanning); independent of `--format`
- `--markdown-file PATH` also write a Markdown report to a file — a fleet summary table (repos × score) plus a findings section per failing repo, suitable to post as a control-repo issue or PR comment; independent of `--format`
- `--format [json|table|both]` output mode (default: `both`)
- `--open-issues` open/update a findings issue on repos that have findings; close it when a repo is compliant. GitHub repos only: GitLab and Gitea repos in a mixed scope are skipped, and a scope without GitHub exits `2`
- `--fail-under FLOAT` exit 1 if any repo scores below this threshold (`0.0`–`1.0`); replaces the default per-check gate
- `--min-coverage FLOAT` exit 1 if any repo's evidence **coverage** is below this threshold (`0.0`–`1.0`); composes with the other gates rather than replacing them. Recommended: `1.0`
- `--public-context` treat output as public: protect private/internal repos per `privacy.private_repos` (default `redact`); overrides `privacy.public_context`. With neither set, on under GitHub Actions and off elsewhere; pass `--public-context=false` to turn it off. See [Privacy guard](configuration.md#privacy-guard)
- `--dry-run` skip API write calls for actions (reads, such as the search for an existing findings issue, still happen)
- `--verbose` debug logging
- `--quiet` suppress table output; keep errors

## Output behavior

- `--format json` prints JSON to stdout unless `--output-file` is set.
- `--format table` prints only the console summary table.
- `--format both` prints JSON and then the table summary.
- `--output-file` is used only when format includes JSON (`json` or `both`).
- Each repo result carries a `forge` field (`github`, `gitlab`, `gitea` or `local`), since one scan can hold the same slug on two forges; SARIF results carry it as a property. A repo the privacy guard masks has none. The table and Markdown report add a forge column only when a scan covers more than one forge.
- `--quiet` suppresses the table summary but does not suppress error messages.
- If both `--verbose` and `--quiet` are set, `--verbose` wins.
- An invalid `--format` (or out-of-range `--fail-under` / `--min-coverage`) value exits with code 2.

## Exit codes

- `0` scan completed and all repos passed — or, with `--fail-under X`, every repo scored `>= X`
- `1` scan completed with one or more failed repos — or, with `--fail-under X`, one or more repos scored below `X`
- `2` runtime/config/auth/discovery error before successful completion — also returned by `privacy.private_repos: fail` when private repos would be disclosed in a public context
- `2` when GitHub refused requests under its API rate limit during the scan, so what they would have read is `unknown` and findings issues they would have written were not; stderr says how many were refused and when the limit resets
- `2` with `--open-issues`, after all output is written, when any findings issue could not be searched for or written. Creating the `baseliner` label counts as a write: an issue is never opened without it, because the label is how later runs find it. Delivery continues for the other repos first, the gate's output is still printed, and this outranks a `1`. Repos that are archived or have Issues disabled are skipped, not counted. `--dry-run` still searches for existing issues, so it catches a token that cannot read them; it cannot catch one that can read but not write, which only a real run exercises.

`--fail-under X` replaces the default per-check gate: a repo with a failing check
still passes as long as its score is `>= X`. Use it for gradual rollout — tolerate
sub-perfect repos above a bar.

## Score and coverage

Each repo reports two independent numbers, and they are deliberately not combined:

- **score** (posture) — the severity-weighted pass ratio over the checks that
  produced a conclusive result: `passed / (passed + failed)`.
- **coverage** — how much of the applicable baseline could be observed at all:
  `(passed + failed) / (passed + failed + unobserved)`.

A check reports `unknown` when it applies but its evidence could not be read: for
example the required git context is unavailable, or on GitHub a directory
listing or the README could not be read (anything but a 404, which means
absent). A file check that would fail for want of a file in a directory that
could not be listed reports `unknown` instead; one that passed, or whose
directories were all read, keeps its result. Unobserved checks reduce
coverage and never raise the score, so missing evidence cannot read as
compliance. Checks that genuinely do not apply report `skip`, and checks a repo
has waived for itself report `waived` with its reason (see
[Repo waivers](policies.md#repo-waivers)); both are excluded from both ratios.

When nothing conclusive was observed, the score is `null` (shown as `n/a`) rather
than `1.0`, and the repo fails the default gate — compliance has to be
demonstrated, not inferred from silence. Partial gaps stay visible as coverage;
gate on them explicitly with `--min-coverage 1.0`.

## GitHub code scanning (SARIF)

`--sarif-file` writes SARIF 2.1.0 (one rule per check, one result per finding)
alongside any other output. Upload it so findings show in the **Security** tab:

```yaml
- name: Scan
  # --public-context keeps private repos out of this run's public log and
  # the SARIF; if this repo is private, use --public-context=false (see
  # Configuration -> Privacy guard).
  run: baseliner scan --config baseliner.yaml --public-context --format table --sarif-file results.sarif
  env:
    GITHUB_TOKEN: ${{ secrets.BASELINER_TOKEN }}

- name: Upload SARIF
  if: always()
  uses: github/codeql-action/upload-sarif@v3
  with:
    sarif_file: results.sarif
```

The job needs `permissions: security-events: write`. Findings are repo-level
(no source line), so they appear as code-scanning alerts without inline
annotation.

## checks

List the built-in checks and the severity they have in the default policy.

```bash
baseliner checks               # table
baseliner checks --format json # machine-readable
```

```text
CHECK                     LAYER  SEVERITY  ENABLED
readme_exists             fs     critical  true
license_exists            fs     high      true
default_branch_is_main    git    medium    true
...
```

The `LAYER` is the repository context a check needs (`fs` / `git` / `platform`);
a check whose layer is unavailable for a repo reports `unknown`, which lowers
coverage and never counts as a pass (see [Score and coverage](#score-and-coverage)).

## policy

Print the **effective** policy for a config — the checks that will run, their
severities, and the ignore rules that suppress them. Useful for debugging "why
didn't check X run on repo Y".

```bash
baseliner policy --config baseliner.yaml
baseliner policy --config baseliner.yaml --format json
```

It resolves `policy.base`, then reports `policy.ignore` (global),
`policy.repo_ignores` (per-repo), `policy.ignore_when` (by visibility) and the
checks repos may waive for themselves (`policy.repo_waivers.allow`, shown as
`repo_waivable` in JSON).

## completion

baseliner ships shell completion via cobra:

```bash
baseliner completion bash      # or zsh | fish | powershell
```

Install per your shell, e.g. for bash:

```bash
baseliner completion bash > /etc/bash_completion.d/baseliner   # system-wide
# or, per-user:
echo 'source <(baseliner completion bash)' >> ~/.bashrc
```

`baseliner completion --help` shows the per-shell instructions.

## Common commands

```bash
# local scan
baseliner scan --config baseliner.yaml --format table

# json artifact + table
baseliner scan --config baseliner.yaml --format both --output-file results.json

# gate CI on a score threshold
baseliner scan --config baseliner.yaml --fail-under 0.8

# inspect the checks and the effective policy
baseliner checks
baseliner policy --config baseliner.yaml
```
