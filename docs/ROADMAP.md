# Roadmap

A living document describing where baseliner is headed. Priorities are a guide,
not a contract — feedback via issues is welcome.

## What baseliner is

Portable assessment-as-code for repository fleets. A single dependency-free
binary scans local checkouts or whole GitHub orgs against a policy you write. It
gives each repo a **0–1 score** over what it could observe and a separate
**coverage** figure for what it could not, and it runs ad hoc, in CI, or
continuously from a control repo with nothing more than a token. No server, no
app to install.

Every org has an implicit baseline ("all our repos should have X"). baseliner
makes it explicit (policy-as-code), measurable (scored), monitored (drift
detection) and honest about evidence (unobserved is `unknown`, never a pass).

**What it checks by default** is repository hygiene and governance (README /
LICENSE / CODEOWNERS / CI / …). **What it is starting to check** is what is
actually enforced:

- branch protection and rulesets, read together;
- bypass actors, including `exempt`;
- plan-gated settings, reported as `unknown`, never as a pass.

That direction comes from
[research into how GitHub reports branch protection](https://cameronbrooks11.github.io/devops/2026/09/12/branch-protection-is-not-where-you-think/).
The first two checks, from [#97](https://github.com/baselinerhq/baseliner/issues/97),
are opt-in and off in the default policy.

### Where it fits (honestly)

The close neighbors are each more mature, and worth knowing before you adopt
anything:

- **OpenSSF Scorecard** — scored, with a *fixed, security-focused* check set. Its
  Branch-Protection check already reads rulesets and bypass actors.
- **GitHub Allstar / OpenSSF Minder** — configurable and fleet-wide *with*
  remediation, but run as a GitHub App / control-plane (Minder needs a server).
  Heavier to adopt; security-leaning.
- **Repolinter** — configurable repo-hygiene linting (the closest fit), but
  unscored — and archived in 2026.
- **GitHub rulesets / custom properties** — native enforcement, but fixed rule
  types and no cross-repo scored report.
- **OPA / Conftest** — a general, mature policy engine; more powerful, but not
  repo-aware and not a product.

baseliner's spot is the *intersection*: **portable (single binary, zero infra)
+ a policy you write + a score that is honest about coverage.** That's a real
but narrow niche, not an empty one. The edge is low adoption friction and
evidence semantics, not breadth or defensibility. We lead with simplicity, and
we'd sooner embed an existing engine than try to out-feature one.

## Principles

1. **Your baseline, as policy.** You compose the baseline — which checks run,
   their severities, per-repo waivers. (User-authored *custom check types* are a
   deliberately gated next step — see the validation gate — not a present claim.)
2. **Integrate where teams already work** — CI exit codes, the GitHub Security tab
   (SARIF), findings issues, the Actions Marketplace.
3. **Report first; remediate only if it earns its place.** Finding a gap is the
   product; auto-fixing it is valuable but is ground Allstar/Minder already hold —
   so it's deferred, not assumed.
4. **A single dependency-free binary at the core** — every feature preserves
   "download one file and run it." Heavier delivery modes (a GitHub App, a
   lightweight dashboard) wrap that core; they don't replace it. This is also the
   project's main survival trait: it keeps re-entry cost low after any dormancy.

## Status

**v0.2.6 (current)** — opt-in forge-control checks that read branch protection
and rulesets together (`default_branch_requires_review`, `no_exempt_bypass`);
`ci_present` no longer passes CI that GitHub isn't running; and a findings issue
that can't be delivered fails the run with exit 2.

**v0.2.5** — released at the validation gate below: evidence coverage (v0.2.3),
privacy-guard and config hardening (v0.2.4), and `GITHUB_API_URL` as the API root
with an end-to-end privacy-guard test (v0.2.5).

**v0.2.2** — actionability: a Markdown fleet report
(`--markdown-file`), per-check policy links (`policy_info` / `policy_url`), and a
presence-check correctness fix (CODEOWNERS in `docs/`). This was the last
unconditional development before the validation gate below.

v0.2.1 added a [privacy guard](configuration.md#privacy-guard)
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
| **v0.2.2** ✅ | **Actionability** | Markdown fleet report + per-check policy links + presence-check correctness — makes the *existing* score actionable. **No engine.** |
| — | **Validation gate** | **Stop building.** Get ≥1 external team running baseliner on a real fleet and saying what's missing — *before* the configurable engine. |
| **v0.3** (gated) | **Configurable checks** | User-authored `file_present`/`file_absent` types spec'd by Repolinter's real ruleset — *only after* validation; corrected design below. |
| **v0.4** (deferred) | Remediate | Optional fix-PRs that respect branch protection — *only if usage warrants*; overlaps Allstar/Minder. |
| north-star (deferred) | App & dashboard | A GitHub App + lightweight dashboard — **only if adoption warrants**; Allstar/Minder already occupy this, evaluate them first. |

**Risk ascends** down the table — small/safe (v0.2) → actionability (v0.2.2) →
configurable engine (v0.3) → writes-to-repos (v0.4) → service/infra (App) — and
the discipline is to **not** climb the ladder ahead of demand. The validation
gate is a real stop, not a figure of speech.

## v0.2.2 — actionability (no engine) — shipped

Three decoupled wins that improve the *existing* tool regardless of where the
niche goes. Settled via an internal red-team (`planning/reconciliation.md`); each
is high value, low risk, and needs **no** configurable engine, collector rewrite,
or schema:

- **Markdown fleet report** — a summary table (repos × score) + per-repo
  status-grouped sections, postable as a control-repo issue or PR comment.
  Borrowed from Repolinter's most-loved output; combined with the score it is
  strictly better than it. Highest value-per-effort, zero coupling.
- **Per-check policy links** (`policy_info` / `policy_url`) — every check can
  carry "why this matters / link to our standard," turning a failing score from a
  *number* into an *action*.
- **Presence-check correctness** — fix the real location bug (CODEOWNERS isn't
  matched in `docs/`) and audit the other built-ins' location tolerance, so
  baseliner's presence checks are at least as correct as the tool it references.

This is the last unconditional development before the gate.

## The validation gate (the hard stop)

After v0.2.2, baseliner is a *complete* tool: a scored, single-binary, fleet
hygiene scanner with actionable reports. **We deliberately stop here and validate
before building the configurable engine.**

Why: the engine + collector rewrite + a config schema is this category's classic
accretion ramp — roughly what Repolinter became before it was archived — and the
variable that actually decides the project isn't expressiveness, it's whether
anyone outside the author wants it. So the gate is explicit:

**Current position: at the gate since 2026-06-18**, when v0.2.2 shipped.

One exception is in scope while the gate holds: **correctness fixes to what
already ships**. v0.2.3 separates posture from evidence coverage, because the
previous scoring treated an unobservable check as compliant and reported a repo
whose evidence could not be read as a perfect 1.00. That is a defect in the
existing tool, not a new capability, so fixing it does not reopen development.

v0.2.4 is the same kind of release. It closes a privacy-guard hole (log lines
printed private repo names in public logs), rejects misspelled config keys that
silently fell back to defaults — including one that turned the guard off — and
stops scanning archived repos by default, since an archived repo that goes stale
fails permanently and nothing can act on it.

- **Stop-dev trigger:** v0.2.2 ships.
- **Resume-dev trigger:** at least one external team runs baseliner on a real
  fleet and articulates a concrete need the current tool can't meet — ideally a
  team that says *why* they'd choose it over GitHub rulesets / Scorecard / Minder.
- **Until then the work is distribution and listening, not code.**
- **One deliberate exception (2026-10):** the #97 spike shipped as two opt-in
  checks rather than a code-only spike, so the research has a working tool to
  point at during outreach. They are off in the default policy, which is
  unchanged. The configurable engine stays behind the gate.

We'd rather one real user shape the engine than design it in a vacuum.

## v0.3 — configurable checks (gated; corrected design)

*Builds only after the gate clears.* The shape is already settled in `planning/`
and corrected by a design red-team, so when it's justified the spec is ready:

- **User-authored check types**, spec'd by Repolinter's real default ruleset (not
  invented): `file_present` / `file_absent` / `directory_present`, with
  `globs_any` / `globs_all` + `nocase`, **multi-location brace globs**, and
  **exclusion** (`!node_modules/**` — without it `file_absent` false-fails on
  vendored binaries).
- **Each configurable check feeds the same severity-weighted score.** The score
  is sacred.
- **Additive, not a rewrite.** The 10 built-ins stay and run alongside; we do
  **not** re-express them (pure regression risk for zero user benefit).
- **The real cost is collector *semantics*, not fetching.** A local walk sees the
  *working tree*; the GitHub Trees API returns *committed files on the default
  branch* — they diverge by construction. Parity needs a gitignore-aware local
  walk that approximates "tracked files," plus a glob dialect that stops at `/`
  (adopt `doublestar`; implement exclusion at the check layer). This — not the
  check syntax — is the work, and it must not silently break source parity.
- **Ship a Repolinter-equivalent default policy**
  (`examples/policies/ospo-baseline.yaml`) so a migrating user gets familiar
  coverage *plus a score, in one binary, across a whole org.*
- A **JSON Schema** for editor help — versioned and evolvable, explicitly **not**
  a "schema-freeze = v1.0" gate. v1.0 follows real-world policies.

Explicitly **out** of the engine: the **axiom subsystem** (language/license
detection — rejected for scope + *silent degradation*, even though pure-Go libs
exist), a **bespoke DSL** (embed OPA/Conftest if real logic is ever needed), and
**Tier-C rule types** (git-grep, file hashes, broken-link checking, json-schema).

## Later / backlog

- **Auto-remediation** fix-PRs (the v0.4 theme): open PRs to add missing
  CODEOWNERS/LICENSE/etc., respecting branch protection. Squarely Allstar/Minder
  territory — adopt or extend before rebuilding.
- **Repo-settings / branch-protection checks** — the governance levers OSPOs
  actually enforce (the score today grades the cheaper half). The
  [#97](https://github.com/baselinerhq/baseliner/issues/97) spike landed two
  opt-in platform checks, `default_branch_requires_review` and
  `no_exempt_bypass`, and showed the collector can reach forge controls: both
  sources, bypass actors, and plan-gated 403s as `unknown`. A full check family
  stays gated like the engine.
- **GitHub App + lightweight dashboard** (north-star, #45) — only if adoption
  warrants; see the releases table.
- **Additional sources** (GitLab/Gitea discovery) — only if demand warrants.
- **Interop output in the OpenSSF Gemara result model** — the format the
  OpenSSF Baseline reference scanner (pvtr) emits, so the natural target if
  results are ever exported for other tools
  ([#101](https://github.com/baselinerhq/baseliner/issues/101)). As of
  [go-gemara](https://github.com/gemaraproj/go-gemara) v0.11.0 (`enums.go`,
  unchanged from v0.10.0):
  - **Result:** `NotRun`, `Passed`, `Failed`, `NeedsReview`, `NotApplicable`,
    `Unknown`. baseliner has no counterpart to `NeedsReview` or `NotRun`.
  - **Mapping:** `pass` → Passed, `fail` → Failed, `skip` → NotApplicable,
    `unknown` and `error` → Unknown. This loses one distinction: baseliner gates
    `error` as a failure, while Gemara aggregates a repo with an Unknown and no
    Failed to Unknown, so the adapter should carry `error` in the message.
  - **ConfidenceLevel** (`Undetermined`, `Low`, `Medium`, `High`) is a
    separate axis; an adapter must not fold it into the result.
  - **`UpdateAggregateResult`:** Failed > Unknown > NeedsReview > Passed >
    NotApplicable, and `NotRun` never overwrites. Unknown outranks Passed, so a
    single unobserved step makes the aggregate Unknown. baseliner shares the
    weaker property, that unobserved evidence never raises a result. On
    unobserved evidence alone, its default gate fails a repo only when nothing
    at all was observed; partial gaps are gated with `--min-coverage`.
  - **Boundary:** interop is an output adapter, never a core runtime
    dependency. Gemara's schemas are still evolving, and the core stays a
    single dependency-free binary (principle 4). Behind the validation gate;
    not a commitment.

## Non-goals

- A **plugin system / arbitrary code execution** in policies — checks stay
  built-in and auditable.
- **Language- or framework-specific linting** — that's dedicated linters' job;
  baseliner checks repository *hygiene and governance*, not code.
- A **heavyweight SaaS UI** — any dashboard stays lightweight (think Renovate's
  Dependency Dashboard issue), and the binary stays usable standalone.
