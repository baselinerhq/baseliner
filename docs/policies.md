# Writing a custom policy

baseliner ships a built-in **default policy** (10 checks). When your org's
baseline differs, point baseliner at your own policy file.

> **Scope today:** a custom policy composes the **built-in checks** — you choose
> which to run, at what severity, and whether each is enabled. Custom *check
> types* (arbitrary file/content/repo-settings checks) are planned for v0.3; see
> the [roadmap](ROADMAP.md).

## Using a custom policy

Set `policy.base` in your `baseliner.yaml` to a path (anything other than
`default` is treated as a file path):

```yaml
scope:
  github:
    type: org
    name: acme
policy:
  base: ./policies/acme.yaml
```

Verify what will actually run:

```bash
baseliner policy --config baseliner.yaml   # the effective policy
baseliner checks                           # the catalog of built-in checks
```

## Policy schema

```yaml
id: acme-v1            # required, non-empty — names the policy
checks:                # required, non-empty
  - id: readme_exists  # must be a built-in check id (see `baseliner checks`)
    severity: critical # critical | high | medium | low
    enabled: true      # optional, defaults to true when omitted
```

- **`id`** — a name for the policy (appears in `baseliner policy` output).
- **`checks[]`** — the checks to run. A check **not listed** simply doesn't run.
- **`severity`** — drives scoring weight (below). An unknown value is treated as
  weight 1.
- **`enabled`** — set `false` to keep a check in the policy but skip it. Omitting
  the key defaults to `true`.

## The built-in checks

Run `baseliner checks` for the live list. As of v0.1.1:

| id | layer | default severity |
|----|-------|------------------|
| `readme_exists` | fs | critical |
| `readme_nonempty` | fs | high |
| `readme_has_heading` | fs | medium |
| `license_exists` | fs | high |
| `gitignore_exists` | fs | medium |
| `ci_present` | fs | high |
| `codeowners_exists` | fs | low |
| `dependency_update_config` | fs | medium |
| `default_branch_is_main` | git | medium |
| `stale_repo` | git | low |

A check's **layer** is the context it needs (`fs` / `git` / `platform`). If that
context isn't available for a repo (e.g. a GitHub repo with no git metadata), the
check is **skipped**, not failed — and skipped checks are excluded from scoring.

## How scoring works

Each repo gets a **severity-weighted pass ratio**:

| severity | weight |
|----------|--------|
| critical | 4 |
| high | 3 |
| medium | 2 |
| low | 1 |

```
score = round( sum(weight of passing checks) / sum(weight of non-skipped checks), 4 )
```

Skipped checks (layer unavailable, disabled, or ignored) are excluded from both
sums. A repo where every check skips scores `1.0`. Use `--fail-under` to gate CI
on this score.

## Ignoring checks per deployment

`enabled: false` lives in the *policy*. To suppress a check without editing the
policy — e.g. for a specific infra repo — use the **config** instead:

```yaml
policy:
  base: ./policies/acme.yaml
  ignore:                       # skip these checks on every repo
    - stale_repo
  repo_ignores:                 # skip per repo (slug -> check ids)
    "acme/.github":
      - ci_present
      - gitignore_exists
```

Ignored checks are skipped (excluded from scoring), exactly like `enabled: false`
— but scoped to the deployment, so the policy stays reusable across orgs.

## Worked examples

See [`examples/policies/`](../examples/policies/):

- [`essentials.yaml`](../examples/policies/essentials.yaml) — a minimal bar (README + LICENSE + CI).
- [`strict.yaml`](../examples/policies/strict.yaml) — all checks, severities raised so nothing is "low".
- [`relaxed.yaml`](../examples/policies/relaxed.yaml) — the full set with `stale_repo` disabled via `enabled: false`.
