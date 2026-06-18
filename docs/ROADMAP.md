# Roadmap

A living document describing where baseliner is headed. Priorities are a guide,
not a contract — feedback via issues is welcome.

## What baseliner is

A single dependency-free binary that scans local checkouts or whole GitHub orgs
against a **configurable** policy, gives each repo a **normalized 0–1 score**, and
reports compliance — ad hoc, in CI, or continuously from a control repo with
nothing more than a token. The bet is **simplicity**: your baseline, a score, no
server and no app to install.

It targets repo *hygiene and governance* (README / LICENSE / CODEOWNERS / CI /
branch protection / …) rather than security or dependencies. Every org has an
implicit baseline ("all our repos should have X"); baseliner makes it explicit
(policy-as-code), measurable (scored), and monitored (drift detection).

### Where it fits (honestly)

This is a crowded space and baseliner does not invent a category. The close
neighbors are each more mature, and worth knowing before you adopt anything:

- **OSSF Scorecard** — scored, but a *fixed, security-specific* check set.
- **GitHub Allstar / OpenSSF Minder** — configurable and fleet-wide *with*
  remediation, but run as a GitHub App / control-plane (Minder needs a server).
  Heavier to adopt; security-leaning.
- **Repolinter** — configurable repo-hygiene linting (the closest fit), but
  unscored — and archived in 2026.
- **GitHub rulesets / custom properties** — native enforcement, but fixed rule
  types and no cross-repo scored report.
- **OPA / Conftest** — a general, mature policy engine; more powerful, but not
  repo-aware and not a product.

baseliner's spot is the *intersection*: **lightweight (single binary, zero
infra) + configurable + scored + hygiene-first.** That's a real but narrow niche,
not an empty one — the edge is low adoption friction and a crisp score, not
breadth or defensibility. We lead with simplicity, and we'd sooner embed an
existing engine than try to out-feature one.

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

**v0.2.1 (current)** — adds a [privacy guard](configuration.md#privacy-guard)
that protects private/internal repos from disclosure when scanning from a public
context. v0.2.0 added `--fail-under` for CI gating, `--sarif-file` for the GitHub
Security tab, `baseliner checks`/`policy` introspection, shell completion, a
[custom-policy authoring guide](policies.md), a [GitHub Action](https://github.com/baselinerhq/baseliner-action)
(`baselinerhq/baseliner-action@v1`), and a docs site at
<https://baselinerhq.github.io>.

Foundation (v0.1): local + GitHub discovery, 10 built-in checks, severity-weighted
scoring, JSON/console output, smart `--open-issues` (open-on-findings,
close-when-compliant), a single static binary, and Homebrew/install-script/`go
install` distribution.

## Releases

| Release | Theme | Headline |
|---|---|---|
| **v0.2** ✅ | Integrate & polish | SARIF, `--fail-under`, Marketplace Action, introspection, policy docs, privacy guard — *usable in real CI* |
| **v0.3** | **Custom checks** | A minimal `file_present` check (#46) — kept small and **validation-gated**; *not* a bespoke policy DSL or a schema freeze (see below) |
| **v0.4** | Remediate | Optional fix-PRs (add CODEOWNERS / LICENSE / …) that respect branch protection — *only if usage warrants*; overlaps Allstar/Minder |
| north-star | App & dashboard | A GitHub App + lightweight dashboard — **only if adoption warrants**; Allstar and Minder already occupy this space, so evaluate them before building it |

Each stage is **gated on the previous one earning it through real use**, and
**risk ascends**: small/safe (v0.2) → custom checks (v0.3) → writes-to-repos
(v0.4) → service/infra (App). The discipline is to *not* climb the risk ladder
ahead of demand — the next unit of work is validation, not construction.

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

## v0.3 — custom checks (deliberately minimal, validation-gated)

The one concretely-requested gap is letting a policy require an **arbitrary file
at an arbitrary path** (#46 — e.g. "every repo must have a `renovate.json`"). The
honest scope is to ship *that* — a `file_present` check type — and little else,
**after** at least one real external user confirms the need.

Explicitly **not** in v0.3 scope (these are how a small tool becomes a stalled
rewrite):

- **Re-expressing the existing 10 checks** on a new engine — pure regression risk
  against the only thing that currently works, with zero user-visible benefit.
- **Freezing the config schema as the v1.0 bar** — no external policy has ever
  been written, so freezing the interface now would lock in a vacuum design.
  v1.0 should follow real-world policies, not invite them.
- A **bespoke policy DSL.** If checks ever need real logic (composition,
  negation, content matching), embedding a mature engine (OPA/Conftest) beats
  hand-rolling and slowly re-deriving a worse Rego.

The real work hiding behind #46 is **collector coverage**: the GitHub collector
currently fetches only a few fixed paths (`.github`, `.github/workflows`, …) and
the local walk is depth-limited, so "arbitrary path" needs a genuine
fetch-strategy change (e.g. the git-tree API) that *also* keeps local and GitHub
results consistent. That — not the check syntax — is the actual cost, and it is
the part to scope honestly.

Larger ideas (repo-settings checks, branch-protection, a content/regex matcher,
JSON-Schema config validation) stay in the backlog until real usage asks for
them.

## Later / backlog

- **Auto-remediation** fix-PRs (the v0.4 theme): open PRs to add missing
  CODEOWNERS/LICENSE/etc. with sensible defaults, respecting branch protection.
  (Motivation: we've done this by hand across whole orgs.) Note this is squarely
  Allstar/Minder territory — adopt or extend before rebuilding.
- **GitHub App + lightweight dashboard** (north-star, #45) — only if adoption
  warrants; see the honesty note in the releases table.
- Additional output formats (Markdown PR-comment summary; severity filtering).
- Additional sources (GitLab/Gitea discovery) — only if demand warrants.

## Non-goals

- A **plugin system / arbitrary code execution** in policies — checks stay
  built-in and auditable.
- **Language- or framework-specific linting** — that's dedicated linters' job;
  baseliner checks repository *hygiene and governance*, not code.
- A **heavyweight SaaS UI** — any dashboard stays lightweight (think Renovate's
  Dependency Dashboard issue), and the binary stays usable standalone.
