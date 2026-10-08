# Roadmap

A living document describing where baseliner is headed. Priorities are a guide,
not a contract — feedback via issues is welcome.

## What baseliner is

Portable assessment-as-code for repository fleets. A single dependency-free
binary scans local checkouts or whole organisations on a forge against a policy
you write: GitHub today, with GitLab and Gitea/Forgejo next. It
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
   their severities, per-repo waivers. (User-authored *custom check types* are
   planned — see [configurable checks](#configurable-checks) — not a present
   claim.)
2. **Integrate where teams already work** — CI exit codes, code-scanning views
   (SARIF), findings issues, the Actions Marketplace.
3. **Report first; remediate only if it earns its place.** Finding a gap is the
   product; auto-fixing it is valuable but is ground Allstar/Minder already hold —
   so it's deferred, not assumed.
4. **A single dependency-free binary at the core** — every feature preserves
   "download one file and run it." Heavier delivery modes (a GitHub App, a
   lightweight dashboard) wrap that core; they don't replace it. This is also the
   project's main survival trait: it keeps re-entry cost low after any dormancy.
5. **Forge-neutral at the core.** Discovery, evidence collection and findings
   delivery sit behind a forge interface, and no check or policy depends on one
   vendor. A forge adds what it can observe; what it cannot is `unknown`.

## Status

**v0.2.8 (current)** — `policy.ignore_when` skips checks by repo visibility
without naming any repo (#98), the first production-readiness slice; a panic, a
missing logger and a mid-scan GitHub rate limit no longer bypass the privacy
guard, hang, or pass silently (#130–#132); and the control-repo template keeps
its weekly schedule from being disabled (#94).

**v0.2.7** — privacy-guard and evidence fixes. The guard: private repo names are redacted in any
letter case and never only in part; `exclude` mode leaves no trace of excluded
repos in logs or gate lists; any visibility other than `public` is protected;
under GitHub Actions it is on unless explicitly turned off; and the control-repo
template enables it. Evidence: a file listing, README or default branch that
could not be read reports `unknown` instead of failing as if absent, so
`--min-coverage 1.0` catches a scan that could not see a repo, and a findings
issue is not closed on evidence that could not be read. Also: no duplicate
findings issues when label creation fails, and git context for linked worktrees.

**v0.2.6** — opt-in forge-control checks that read branch protection
and rulesets together (`default_branch_requires_review`, `no_exempt_bypass`);
`ci_present` no longer passes CI that GitHub isn't running; and a findings issue
that can't be delivered fails the run with exit 2.

**v0.2.5** — evidence coverage (v0.2.3),
privacy-guard and config hardening (v0.2.4), and `GITHUB_API_URL` as the API root
with an end-to-end privacy-guard test (v0.2.5).

**v0.2.2** — actionability: a Markdown fleet report
(`--markdown-file`), per-check policy links (`policy_info` / `policy_url`), and a
presence-check correctness fix (CODEOWNERS in `docs/`).

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

## Production readiness

What gets built next is decided by one question: **what would a team running
baseliner in production, on its whole fleet, expect and need?** The work is
tracked in [the production-readiness epic (#143)](https://github.com/baselinerhq/baseliner/issues/143).
By area:

- **Trust and security.** Releases a user can verify came from this repo's
  source ([#138](https://github.com/baselinerhq/baseliner/issues/138)); the least
  privilege each feature needs, documented and verified
  ([#139](https://github.com/baselinerhq/baseliner/issues/139)); and a privacy
  guard that holds in every mode and output.
- **Correct evidence.** On every source, what could not be read is `unknown`,
  never a pass, and never a failure as if the file were missing.
- **Usable policy.** Checks that can be scoped by repo visibility
  ([#98](https://github.com/baselinerhq/baseliner/issues/98)) and waived by the
  repo they excuse ([#103](https://github.com/baselinerhq/baseliner/issues/103)),
  without naming a private repo in public config; and
  [configurable checks](#configurable-checks)
  ([#46](https://github.com/baselinerhq/baseliner/issues/46)).
- **Platform reach.** GitLab ([#140](https://github.com/baselinerhq/baseliner/issues/140))
  and Gitea/Forgejo, including Codeberg
  ([#141](https://github.com/baselinerhq/baseliner/issues/141)), behind one forge
  interface, then findings issues on each
  ([#142](https://github.com/baselinerhq/baseliner/issues/142)).
- **Operable delivery.** Authentication without a personal token
  ([#45](https://github.com/baselinerhq/baseliner/issues/45)), schedules that keep
  running, and runs that say why they are incomplete.

The discipline that came with the old validation gate still holds: features are
built where a production need names them, not speculatively. Configurable
checks, a schema and remediation are this category's accretion ramp, roughly
what Repolinter became before it was archived. Each one is taken on when a
concrete production gap calls for it, as the smallest change that closes the
gap, and the score and the single binary stay intact.

*History:* from v0.2.2 (2026-06-18) until 2026-10-08, development was held at a
"validation gate" that allowed only correctness fixes until an external team
asked for more. Production readiness replaced it as the deciding question.

## Configurable checks

Tracked as [#46](https://github.com/baselinerhq/baseliner/issues/46). The shape is
already settled in `planning/` and corrected by a design red-team:

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

- **Auto-remediation** fix-PRs: open PRs to add missing
  CODEOWNERS/LICENSE/etc., respecting branch protection. Squarely Allstar/Minder
  territory — adopt or extend before rebuilding.
- **Repo-settings / branch-protection checks** — the governance levers OSPOs
  actually enforce (the score today grades the cheaper half). The
  [#97](https://github.com/baselinerhq/baseliner/issues/97) spike landed two
  opt-in platform checks, `default_branch_requires_review` and
  `no_exempt_bypass`, and showed the collector can reach forge controls: both
  sources, bypass actors, and plan-gated 403s as `unknown`. A full check family
  follows once the forge interface (#140) defines what each forge can observe.
- **A lightweight dashboard** over aggregated history — the second half of #45,
  once authentication without a personal token lands.
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
    single dependency-free binary (principle 4). Not scheduled; not a
    commitment.

## Non-goals

- A **plugin system / arbitrary code execution** in policies — checks stay
  built-in and auditable.
- **Language- or framework-specific linting** — that's dedicated linters' job;
  baseliner checks repository *hygiene and governance*, not code.
- A **heavyweight SaaS UI** — any dashboard stays lightweight (think Renovate's
  Dependency Dashboard issue), and the binary stays usable standalone.
