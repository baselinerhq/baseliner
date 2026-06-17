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
- `--format [json|table|both]` output mode (default: `both`)
- `--open-issues` open/update a findings issue on repos that have findings; close it when a repo is compliant
- `--fail-under FLOAT` exit 1 if any repo scores below this threshold (`0.0`–`1.0`); replaces the default per-check gate
- `--dry-run` skip API write calls for actions
- `--verbose` debug logging
- `--quiet` suppress table output; keep errors

## Output behavior

- `--format json` prints JSON to stdout unless `--output-file` is set.
- `--format table` prints only the console summary table.
- `--format both` prints JSON and then the table summary.
- `--output-file` is used only when format includes JSON (`json` or `both`).
- `--quiet` suppresses the table summary but does not suppress error messages.
- If both `--verbose` and `--quiet` are set, `--verbose` wins.
- An invalid `--format` (or out-of-range `--fail-under`) value exits with code 2.

## Exit codes

- `0` scan completed and all repos passed — or, with `--fail-under X`, every repo scored `>= X`
- `1` scan completed with one or more failed repos — or, with `--fail-under X`, one or more repos scored below `X`
- `2` runtime/config/auth/discovery error before successful completion

`--fail-under X` replaces the default per-check gate: a repo with a failing check
still passes as long as its score is `>= X`. Use it for gradual rollout — tolerate
sub-perfect repos above a bar.

## GitHub code scanning (SARIF)

`--sarif-file` writes SARIF 2.1.0 (one rule per check, one result per finding)
alongside any other output. Upload it so findings show in the **Security** tab:

```yaml
- name: Scan
  run: baseliner scan --config baseliner.yaml --format table --sarif-file results.sarif
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
a check whose layer is unavailable for a repo is skipped, not failed.

## policy

Print the **effective** policy for a config — the checks that will run, their
severities, and the ignore rules that suppress them. Useful for debugging "why
didn't check X run on repo Y".

```bash
baseliner policy --config baseliner.yaml
baseliner policy --config baseliner.yaml --format json
```

It resolves `policy.base`, then reports `policy.ignore` (global) and
`policy.repo_ignores` (per-repo).

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
