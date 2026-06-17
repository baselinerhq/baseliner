# Getting Started

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/baselinerhq/baseliner/main/scripts/install.sh | bash
```

This installs the `baseliner` binary to `~/.local/bin`. See [Install](install.md) for other
options (`go install`, prebuilt archives, Homebrew). No runtime is required.

## First scan (local repo)

Create `baseliner.yaml`:

```yaml
scope:
  local:
    paths:
      - .
policy:
  base: default
```

Run:

```bash
baseliner scan --config baseliner.yaml --format table
```

## Scan GitHub repositories

Start from the provided example:

```bash
cp examples/baseliner.yaml baseliner.yaml
```

Edit `scope.github` (`type`, `name`) and then run:

```bash
export GITHUB_TOKEN=<your_pat>
baseliner scan --config baseliner.yaml --format both --output-file results.json
```

Use `--open-issues` to create/update a `[baseliner]` findings issue in each GitHub repo.

## Next steps

- Tune the checks to your org's baseline — see [Writing a custom policy](policies.md).
- Run continuously on a schedule — see [Control Repo](control-repo.md).
