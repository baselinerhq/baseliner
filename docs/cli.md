# CLI Reference

## Root command

```bash
baseliner --help
```

Global options:

- `--version` show version and exit
- `--help` show help

## scan

```bash
baseliner scan --help
```

Options:

- `--config PATH` path to config file (default: `baseliner.yaml`)
- `--output-file PATH` write JSON output to a file
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

## Common commands

```bash
# local scan
baseliner scan --config baseliner.yaml --format table

# json artifact + table
baseliner scan --config baseliner.yaml --format both --output-file results.json

# open issues without writes
baseliner scan --config baseliner.yaml --open-issues --dry-run --format table
```
