# Writing a custom policy

baseliner ships a built-in **default policy**: 12 checks, of which the 10
file and git checks are enabled. When your org's
baseline differs, point baseliner at your own policy file.

> **Scope today:** a custom policy composes the **built-in checks** — you choose
> which to run, at what severity, and whether each is enabled — and can define
> [file-presence checks](#checks-your-policy-defines) of its own. Other check
> types (globs, absent files, file content, repo settings) are planned; see
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
  - id: readme_exists  # a built-in check id (see `baseliner checks`), or a
                       # check the policy defines (see below)
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
a directory whose listing could not be read reports `unknown` too. That
includes a directory of 1000 or more entries, as many as GitHub lists.

## Checks your policy defines

A check with `type: file_present` passes when any of the files it lists
exists. It takes the same `severity`, `enabled`, `policy_info` and
`policy_url` keys as a built-in check:

```yaml
checks:
  - id: renovate_config
    type: file_present
    severity: medium
    any_of: [renovate.json, .github/renovate.json]
```

- **`id`** — a name of lowercase letters, digits, `_`, `.` and `-` that is
  not a built-in check's id. It is used like one: in `ignore`,
  `repo_ignores`, `ignore_when` and `repo_waivers.allow`.
- **`any_of`** — exact paths from the repo root, at most four segments deep
  (the depth a local scan reads), and not inside `.git`. No globs: list each
  place the file may be. The enabled checks' paths may sit in at most 20
  directories, since each is one more request per repo on a forge.
  Matching is case-sensitive. A directory or submodule at a listed path
  does not count; a symlink counts in a local scan but not on a forge, as
  for the built-in file checks.
- **Evidence.** baseliner lists each directory a listed path sits in,
  unless the check is in `ignore`. A file found passes; when none is found
  and every one of those directories was read, the check fails; when one
  could not be read, it is `unknown`. On GitHub, a directory of 1000 or more
  entries counts as not read in full, since that is as many as GitHub
  lists. What those listings find is this check's evidence only: a README
  or LICENSE there does not change a built-in check.

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
- A check a repo has waived for itself reports `waived`, with the repo's reason
  as its message. It too is out of both numbers, and it is not a failure.

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
  ignore_when:                  # skip by repo visibility, naming no repo
    - visibility: [private, internal]
      checks: [license_exists]
```

Ignored checks do not run and produce no result, exactly like `enabled: false`
— but scoped to the deployment, so the policy stays reusable across orgs.

`ignore_when` suits checks that matter only for some visibilities: a LICENSE
usually matters on a public repo and not on a private one. Since it names no
repo, it is also how a public control repo waives a check for its private
repos.

Visibility means who can see the repo, whatever the forge calls it:

| Value | Who can see the repo |
|---|---|
| `public` | anyone, signed in or not |
| `internal` | any signed-in user of the instance or enterprise |
| `private` | only its members or collaborators |

On GitHub these are the repo's own visibility values. A GitHub Enterprise Server
version that does not report visibility shows an internal repo as `private`, so
list both if you mean both. A local repo has no visibility, so no rule applies to
it.

How rules apply:

- A rule matches a repo when the repo's visibility is any of the rule's values.
- A check is skipped if any matching rule lists it; rules add to `ignore` and
  `repo_ignores`, never subtract.
- An unknown visibility or check ID, or a rule missing either list, is a config
  error, so a typo cannot quietly waive nothing.

This is a deployment-level ignore, like `ignore`: a skipped check leaves no
result and no record. A waiver that is recorded with its reason, declared by
the repo it excuses, is a [repo waiver](#repo-waivers).
`baseliner policy` lists the rules.

## Repo waivers

A repo can waive a check for itself, with a reason, in a `.baseliner.yml` at its
root:

```yaml
version: 1 # optional; the file format version
waivers:
  - check: ci_present
    reason: docs only, nothing to build
  - check: license_exists
    reason: internal tooling, not distributed
    until: 2027-01-01 # optional: the last day it applies
```

`.baseliner.yaml` works too; a repo with both is ambiguous, and neither is read.

The central policy decides which checks a repo may waive:

```yaml
policy:
  repo_waivers:
    allow: [ci_present, license_exists]
```

- **Off unless allowed.** Without `policy.repo_waivers`, or for a check
  `allow` does not list, a repo's waiver is ignored with a warning and the
  check runs. Leave a check out of `allow` to make it unwaivable.
- **Recorded, not dropped.** A waived check is reported with status `waived`
  and the repo's reason: in JSON, in the Markdown report's Waivers section,
  and in the repo's findings issue. Like `skip`, it counts toward neither
  score nor coverage, and it is not a failure.
- **Expiry.** A waiver past its `until` date (UTC) stops applying, and the
  check runs again.
- **Strict file.** An unknown key, a second YAML document, a waiver without a
  check or a reason, a reason over 300 characters, a check waived twice, an
  `until` that is not a `YYYY-MM-DD` date, or a file over 64 KiB makes the
  whole file invalid: its waivers are ignored with a warning, and the checks
  run. A `version` newer than this baseliner reads is reported as such, so an
  older scanner says why it ignores a newer file.
- **Where it is read.** On GitHub, from the default branch, as stored in the
  repo: a symlink there is read as its link text, which is not a valid file,
  and is never followed. Locally, only as a regular file in the repo: a
  symlink, pipe or other special file is refused, without following or
  waiting on it. A repo with both `.baseliner.yml` and `.baseliner.yaml` is
  ambiguous, and neither is read.
- **Reasons are shown as plain text.** In Markdown reports and findings issues
  every check message, a waiver reason included, is shown as code: one code
  span per cell, with line breaks turned into spaces and control and
  invisible format characters dropped. So a reason cannot start a new row,
  open HTML, form a link, image, emoji or math, reference an issue, pull
  request or commit, or mention anyone.
- **SARIF.** A waived check is not a finding, so it is not in the SARIF file;
  an alert raised for it on an earlier run closes as fixed.
- **Private stays private.** The file lives in the repo, so a private repo's
  waivers are never named in a public control repo's config. In a public
  context the privacy guard treats the reason like any other message from a
  private repo: blanked in `redact` mode, absent in `exclude` mode.
- Central ignores (`ignore`, `repo_ignores`, `ignore_when`) apply first; a
  repo waiver only covers a check that would otherwise run.

## Worked examples

See [`examples/policies/`](../examples/policies/):

- [`essentials.yaml`](../examples/policies/essentials.yaml) — a minimal bar (README + LICENSE + CI).
- [`strict.yaml`](../examples/policies/strict.yaml) — every file and git check, severities raised so nothing is "low".
- [`relaxed.yaml`](../examples/policies/relaxed.yaml) — the default file and git checks, with `stale_repo` disabled via `enabled: false`.
- [`forge-controls.yaml`](../examples/policies/forge-controls.yaml) — only the two platform checks: what protects each default branch, and any `exempt` bypass.
