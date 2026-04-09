# CLI Reference

## Root command

```bash
uv run baseliner --help
```

Global options:

- `--version` show version and exit
- `--help` show help

## scan

```bash
uv run baseliner scan --help
```

Options:

- `--config PATH` path to config file (default: `baseliner.yaml`)
- `--output-file PATH` write JSON output to a file
- `--format [json|table|both]` output mode (default: `both`)
- `--open-issues/--no-issues` open/update findings issues in GitHub repos
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

## Exit codes

- `0` scan completed and all repos passed
- `1` scan completed with one or more failed repos
- `2` runtime/config/auth/discovery error before successful completion

In default mode, unexpected errors are summarized in one line. With `--verbose`,
tracebacks are also logged.

## Common commands

```bash
# local scan
uv run baseliner scan --config baseliner.yaml --format table

# json artifact + table
uv run baseliner scan --config baseliner.yaml --format both --output-file results.json

# open issues without writes
uv run baseliner scan --config baseliner.yaml --open-issues --dry-run --format table
```
