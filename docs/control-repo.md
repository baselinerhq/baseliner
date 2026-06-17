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
5. Trigger `workflow_dispatch`.
6. Confirm:
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
