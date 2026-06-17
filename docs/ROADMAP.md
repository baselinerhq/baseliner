# Roadmap

This is a living document describing where baseliner is headed. Priorities are a
guide, not a contract — feedback via issues is welcome.

## North star

baseliner should be the **drop-in baseline-compliance scanner for a fleet of
repositories**: a single binary you can run locally or in CI, that scans local
checkouts or whole GitHub orgs against a **configurable policy** and reports
findings in formats your existing tooling already understands.

Three themes drive prioritization:

1. **Integrate where teams already work** — CI, the GitHub Security tab, the
   Actions Marketplace. Output should flow into existing dashboards, not a new one.
2. **Make the policy first-class** — the engine already supports custom policies;
   authoring, validating, and extending them should be easy and well-documented.
3. **Stay a single, dependency-free binary** — every feature must preserve the
   "download one file and run it" property.

## Status

**v0.1.0 (shipped)** — local + GitHub discovery, 10 built-in checks,
severity-weighted scoring, JSON/console output, optional findings issues, single
static binary, Homebrew/install-script/`go install` distribution.

## v0.2 — proposed scope

Ordered by value × effort. Each item below has (or will have) a tracking issue.

| # | Feature | Why | Effort |
|---|---------|-----|--------|
| 1 | **SARIF output** (`--format sarif` / `--sarif-file`) | Uploads to GitHub code scanning → findings appear in the Security tab and PR annotations. The single biggest CI-integration win. | M |
| 2 | **Marketplace GitHub Action** | A `uses: baselinerhq/baseliner-action@v1` step instead of curl-install boilerplate. Removes the main adoption friction for CI users. | M |
| 3 | **Custom-policy authoring guide + examples** | The engine already loads `policy.base: <path>`; document the schema, severities, and enable/disable semantics with worked examples. Unlocks the "configurable" promise. | S |
| 4 | **`--fail-under <score>`** | Let CI gate on an aggregate/score threshold instead of only the binary pass/fail. Common request for gradual rollout. | S |
| 5 | **Shell completion** (`baseliner completion …`) | Nearly free via cobra; improves day-to-day ergonomics. | S |
| 6 | **Introspection commands** (`baseliner checks`, `baseliner policy`) | List available checks and print the effective/merged policy — helps users author policies and debug scope. | S–M |

## Backlog (post-v0.2, not yet scheduled)

- **More checks** (platform layer): branch protection enabled, `SECURITY.md` /
  `CODE_OF_CONDUCT.md` / `CONTRIBUTING.md` present, repo description & topics set,
  default-branch protection, signed-commit policy. These need the GitHub API
  `PlatformContext`, which is currently stubbed.
- **JSON Schema** for `baseliner.yaml` and policy files (editor autocompletion +
  pre-flight validation with precise errors).
- **Per-check configuration** (e.g. configurable stale threshold, required-file
  lists) without writing a full custom policy.
- **Additional output formats** (Markdown summary for PR comments; severity
  filtering).
- **Additional sources** (GitLab/Gitea discovery) — only if demand warrants it.

## Non-goals

- A hosted service or web dashboard — baseliner stays a CLI.
- A plugin system / arbitrary code execution in policies — checks stay built-in
  and auditable.
- Language- or framework-specific linting — that's the job of dedicated linters;
  baseliner checks repository *hygiene and governance*.
