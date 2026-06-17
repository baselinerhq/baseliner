# Roadmap

A living document describing where baseliner is headed. Priorities are a guide,
not a contract — feedback via issues is welcome.

## What baseliner is

A fleet-wide **repository governance baseline** scanner: a single binary that
scans local checkouts or whole GitHub orgs against a configurable policy, scores
each repo, and reports compliance — run ad hoc, in CI, or continuously from a
control repo.

The shortest way to place it: **"Renovate, but for repository governance."** Same
operating model as Renovate — fleet-wide, control-repo/app-driven, scheduled,
with a dashboard and auto-remediation on the horizon — pointed at repo *hygiene
and governance* (README / LICENSE / CODEOWNERS / CI / branch protection / …)
instead of dependencies. Every org has an implicit baseline ("all our repos
should have X"); baseliner makes it **explicit** (policy-as-code), **measurable**
(scored), and **monitored** (drift detection).

Neighbors, and the wedge: **OSSF Scorecard** is security-specific with a fixed
check set; **GitHub rulesets** are GitHub-native with limited check types and no
fleet scoring/reporting. baseliner's niche is **configurable-baseline-first +
fleet + scored + control-repo.**

## Principles

1. **Your baseline, as code.** The policy is the product — users declare their own
   checks; baseliner isn't "ten opinions."
2. **Integrate where teams already work** — CI exit codes, the GitHub Security tab
   (SARIF), findings issues, the Actions Marketplace.
3. **Report, then remediate.** Finding a gap is half; opening the PR to fix it is
   what saves real work.
4. **A single dependency-free binary at the core** — every feature preserves
   "download one file and run it." Heavier delivery modes (a GitHub App, a
   lightweight dashboard) wrap that core; they don't replace it.

## Status

**v0.1.1 (current)** — local + GitHub discovery, 10 built-in checks,
severity-weighted scoring, JSON/console output, single static binary,
Homebrew/install-script/`go install` distribution. `--open-issues` opens a
findings issue only on repos with findings and closes it when they become
compliant.

## Releases

| Release | Theme | Headline |
|---|---|---|
| **v0.2** | Integrate & polish | SARIF, `--fail-under`, Marketplace Action, introspection, policy docs — *usable in real CI* |
| **v0.3** | **Policy engine** | Declarative/custom checks (file, content, repo-settings, branch-protection) + a policy schema — *your baseline, as code*; the path to a stable v1.0 config |
| **v0.4** | Remediate | Renovate-style fix-PRs (add CODEOWNERS / LICENSE / …) that respect branch protection — *fix my fleet* |
| north-star | App & dashboard | GitHub App (no PAT, webhooks, required-check), a lightweight fleet-health dashboard, Marketplace — *install-and-go* |

Each stage unlocks the next, and **risk ascends**: small/safe (v0.2) →
architectural (v0.3) → writes-to-repos (v0.4) → service/infra (App). Ship the
cheap, adoption-enabling release first and let real usage validate each leap.

## v0.2 — integrate & polish

Ordered by value × effort. Tracked under epic #38.

| # | Feature | Why | Effort |
|---|---------|-----|--------|
| #32 | **SARIF output** | Uploads to GitHub code scanning → findings in the Security tab + PR annotations. Biggest CI-integration win. | M |
| #33 | **Marketplace GitHub Action** | `uses: baselinerhq/baseliner-action@v1` instead of curl-install boilerplate. | M |
| #34 | **Custom-policy authoring guide** | Document the policy schema, severities, enable/disable, with worked examples. | S |
| #35 | **`--fail-under <score>`** | Gate CI on a score threshold, not just binary pass/fail. (Confirmed by real use — a fleet of 0.96s read as "failed".) | S |
| #36 | **Shell completion** | Nearly free via cobra. | S |
| #37 | **Introspection** (`checks` / `policy`) | List checks; print the effective policy — helps authoring + debugging. | S–M |

## v0.3 — policy engine (next large epic)

Turn the ten hardcoded detectors into a **declarative policy with custom check
types.** This is the identity-defining release and the prerequisite for
remediation. Pieces:

- **Declarative check types** — file-presence with location constraints (#46),
  content/regex match, repo-settings (topics/description/visibility),
  **branch-protection enabled**; the existing hygiene checks re-expressed on the
  same engine.
- **Collector coverage** for arbitrary paths — the gating work: the local walk is
  depth-limited and the GitHub-API collector shallow-fetches a few fixed paths.
- **Policy schema + validation** — JSON Schema for `baseliner.yaml`/policy files,
  precise errors, editor autocomplete (companion to #34).
- **Per-check configuration** (e.g. stale threshold, required-file lists) without
  a full custom policy.

Stabilizing this config schema is the bar for a **v1.0**.

## Later / backlog

- **Auto-remediation** fix-PRs (the v0.4 theme): open PRs to add missing
  CODEOWNERS/LICENSE/etc. with sensible defaults, respecting branch protection.
  (Motivation: we've done this by hand across whole orgs.)
- **GitHub App + lightweight dashboard** (north-star, #45); **privacy guard** for
  public scanning contexts (#44).
- Additional output formats (Markdown PR-comment summary; severity filtering).
- Additional sources (GitLab/Gitea discovery) — only if demand warrants.

## Non-goals

- A **plugin system / arbitrary code execution** in policies — checks stay
  built-in and auditable.
- **Language- or framework-specific linting** — that's dedicated linters' job;
  baseliner checks repository *hygiene and governance*, not code.
- A **heavyweight SaaS UI** — any dashboard stays lightweight (think Renovate's
  Dependency Dashboard issue), and the binary stays usable standalone.
