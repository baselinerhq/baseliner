# Writing a custom policy

baseliner ships a built-in **default policy**: 12 checks, of which the 10
file and git checks are enabled. When your org's
baseline differs, point baseliner at your own policy file.

> **Scope today:** a custom policy composes the **built-in checks** — you choose
> which to run, at what severity, and whether each is enabled. Custom *check
> types* (arbitrary file/content/repo-settings checks) are planned; see
> [configurable checks](ROADMAP.md#configurable-checks) in the roadmap.

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
    policy_info: "Every repo must have a README."   # optional rationale
    policy_url: "https://wiki.acme.example/standards/readme"  # optional link
```

- **`id`** — a name for the policy (appears in `baseliner policy` output).
- **`checks[]`** — the checks to run. A check **not listed** simply doesn't run.
- **`severity`** — drives scoring weight (below). An unknown value is treated as
  weight 1.
- **`enabled`** — set `false` to keep a check in the policy without running it. Omitting
  the key defaults to `true`.
- **`policy_info`** *(optional)* — a short "why this matters", shown on findings
  in the console and the Markdown report, and carried in JSON.
- **`policy_url`** *(optional)* — a link to your governing standard; the check id
  links to it in the Markdown report. The built-in policy points each check at
  the [checks reference](#the-built-in-checks); set your own to point at your
  org's standards.

## The built-in checks

Run `baseliner checks` for the live list:

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
| `default_branch_requires_review` | platform | high — **off in the default policy** |
| `no_exempt_bypass` | platform | high — **off in the default policy** |

`ci_present` passes when at least one CI file is running. On GitHub sources it
reads workflow state from the Actions API: workflows disabled for inactivity or
by hand don't count, and on a fork a workflow file counts only if GitHub lists
it as `active`, so a fork whose Actions were never enabled fails. On a non-fork
a workflow GitHub doesn't list still counts, because GitHub lists a workflow
only once an event or a push to the file has reached it. Where that state can't be
read in full (a token without Actions: Read, or a local checkout) it falls back
to file presence, which passes disabled workflows; a GitHub scan logs one
warning per run, not per repo, when that happens. That pass counts as full
coverage, so `--min-coverage` does not catch it.

The two **platform** checks read what protects the default branch on GitHub. They
cost extra API calls per repo, so they only run when a policy enables them:

- **`default_branch_requires_review`** reads classic branch protection and
  rulesets together, because GitHub reports them from two endpoints that do not
  reference each other. It passes when either requires at least one approving
  review. A source that cannot be read (e.g. a plan-gated 403) can never turn the
  answer into "no review". When the other source doesn't settle it, the result
  is `unknown`.
- **`no_exempt_bypass`** fails when a ruleset on the default branch has a bypass
  actor in `exempt` mode. For that actor rules are not run and, per GitHub's API
  spec, no bypass audit entry is created. Bypass actors are only visible to
  tokens with admin access; without it the check is `unknown`, never a pass.

**Token:** both checks want admin access to each repo. Without it, classic branch
protection reads as a generic 404 and is reported unreadable, so on a repo
protected only by classic rules the review check is `unknown`. The rules view
itself needs only read access on public repos.

Messages count bypass actors by mode (e.g. `bypass: 1 always, 1 exempt`) and
never name who can bypass: GitHub shows that to admins only, and findings can
land somewhere public, such as a public control repo's log or a findings issue on
a public repo.

Their messages carry the evidence: each source's state, every applicable
ruleset, its approval count and its bypass actors counted by mode. A passing
`no_exempt_bypass` carries no message.

A check's **layer** is the context it needs (`fs` / `git` / `platform`). If that
context isn't available for a repo (e.g. a local checkout has no platform
context), the check reports **`unknown`**: it applies, but its evidence could not
be read. That lowers the repo's coverage and never counts as a pass. The same
applies within a layer: on GitHub, a file check that fails for want of a file in
a directory whose listing could not be read reports `unknown` too.

## How scoring works

Each repo gets two numbers, deliberately kept apart. Both are weighted by
severity:

| severity | weight |
|----------|--------|
| critical | 4 |
| high | 3 |
| medium | 2 |
| low | 1 |

```
score    = round( weight(pass) / weight(pass + fail), 4 )
coverage = round( weight(pass + fail) / weight(pass + fail + unknown + error), 4 )
```

- **score** (posture) grades only what was conclusively observed.
- **coverage** says how much of the applicable baseline could be observed at all.
- When nothing conclusive was observed, the score is `null` (shown as `n/a`), not
  `1.0`, and the repo fails the default gate and any `--fail-under`.
- Disabled and ignored checks never run, so they produce no result at all. A repo
  whose every check is disabled or ignored therefore has nothing conclusive and
  fails like any other unassessed repo.
- The model also has a `skip` status (the check does not apply), which is out of
  both numbers. No built-in check reports it today.

Use `--fail-under` to gate CI on the score and `--min-coverage` to gate on
coverage. See [CLI → Score and coverage](cli.md#score-and-coverage).

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

Ignored checks do not run and produce no result, exactly like `enabled: false`
— but scoped to the deployment, so the policy stays reusable across orgs.

## Worked examples

See [`examples/policies/`](../examples/policies/):

- [`essentials.yaml`](../examples/policies/essentials.yaml) — a minimal bar (README + LICENSE + CI).
- [`strict.yaml`](../examples/policies/strict.yaml) — every file and git check, severities raised so nothing is "low".
- [`relaxed.yaml`](../examples/policies/relaxed.yaml) — the default file and git checks, with `stale_repo` disabled via `enabled: false`.
- [`forge-controls.yaml`](../examples/policies/forge-controls.yaml) — only the two platform checks: what protects each default branch, and any `exempt` bypass.
