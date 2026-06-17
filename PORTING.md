# Porting baseliner to Go

This document scopes and tracks the rewrite of `baseliner` from Python to Go.

## Why Go

`baseliner` is a CLI meant for **wide adoption as a standalone tool** — installed and
run in CI against a fleet of repositories. The current Python implementation carries a
runtime-dependency tax (`uv tool install` requires Python + uv on every runner/machine).
Go ships a single static, cross-compiled binary with no runtime, which is the dominant
distribution model for this class of tool.

Reasons, in order of weight:

1. **Distribution.** One static binary (`go install`, Homebrew, `curl` an install script,
   or a GitHub Action that just downloads it) replaces `setup-uv` + `uv sync`.
2. **Ecosystem fit.** The closest prior art — OSSF Scorecard, `gh`, `trivy`, `gitleaks` —
   are all Go. Repo-fleet scanning over the GitHub API is squarely Go's niche.
3. **Concurrency.** Fleet scanning is embarrassingly parallel I/O. Python scans serially;
   Go fans out cheaply (bounded `errgroup`). The real ceiling is GitHub's rate limit
   (5k req/hr), so this is a latency win, not a throughput unlock.

The port is cheap **now**: the project is `0.1.0`, ~1,500 LOC, pre-PyPI, with clean
`Discovery → Collection → Evaluation → Output → Actions` layer separation that maps to Go
almost 1:1. This is "translate + re-idiom", not "redesign".

## Dependency / idiom mapping

| Python | Go | Notes |
|---|---|---|
| `typer` | `spf13/cobra` (+`pflag`) | Direct mapping. |
| `pydantic` | structs + `encoding/json` + `gopkg.in/yaml.v3` | No 1:1. Lose free validation/coercion and `model_copy`; biggest idiom shift. |
| `pygithub` | `google/go-github` + `golang.org/x/oauth2` | Mature, well-typed; mechanical but verbose. |
| `gitpython` | `go-git/go-git` *or* shell out to `git` | The one real risk — see Decisions. |
| `pyyaml` | `gopkg.in/yaml.v3` | Direct. |
| `importlib.resources` (ship `default.yaml` in wheel) | `//go:embed` | Cleaner; deletes the CI wheel-data integrity check. |
| colored console | `fatih/color` or `charmbracelet/lipgloss` | Direct. |
| `uv tool install` | `goreleaser` + Homebrew tap + install script | Net-new, but it's the point of the port. |

## Key decisions

1. **MVP is GitHub-API-only.** The local-git collector (the highest-risk component) is
   deferred to Phase 2. This removes the riskiest piece from the critical path and matches
   how the tool is actually used (CI against an org). A go-git prototype gates whether
   local-git stays go-git or shells out to `git`.
2. **No pydantic.** Two spots need rework, not just translation: the `model_copy(update=...)`
   calls in `engine.py` (severity override) and `cli.py` (merging git context into the fs
   result). In Go these become explicit struct construction. Validation moves from free to a
   small explicit pass.
3. **Bank the concurrency win.** Add bounded `errgroup` fan-out across repos in Phase 2.

## Phasing

- **Phase 1 — Parity core / MVP (~8–10 engineer-days):** scaffold → models → config →
  checks → filesystem + GitHub-API collectors → engine → output → CLI. GitHub-only. Ships a
  working `scan` against an org with JSON + table output and correct exit codes.
- **Phase 2 — Feature parity (~4–5 days):** GitHub-issues action, local-git collector,
  concurrency fan-out, full test port (82 tests → table-driven).
- **Phase 3 — Distribution (~2–3 days):** goreleaser multi-platform binaries, Homebrew tap,
  install script, a download-only GitHub Action. The payoff phase.

**Total: ~15–22 engineer-days (~3–4 focused weeks).**

## Behavior to preserve (parity contract)

- 10 default checks with severities: `readme_exists` (critical), `readme_nonempty` (high),
  `readme_has_heading` (medium), `license_exists` (high), `gitignore_exists` (medium),
  `ci_present` (high), `codeowners_exists` (low), `dependency_update_config` (medium),
  `default_branch_is_main` (medium), `stale_repo` (low, 90-day threshold).
- Severity-weighted score: critical=4, high=3, medium=2, low=1; `passed_weight/total_weight`
  rounded to 4 dp; skipped checks excluded; all-skipped ⇒ 1.0.
- Layer-skip: a check whose required layer (`fs`/`git`) is absent is skipped, not failed.
- Exit codes: `0` all pass · `1` ≥1 repo failed · `2` config/auth/runtime error.
- Idempotent per-repo findings issue: one issue identified by title + `baseliner` label,
  updated (not duplicated) on re-runs; honors `--dry-run`.
- Config schema (`baseliner.yaml`) and the `default-v1` policy remain unchanged so existing
  control repos keep working.

## Validation

The `baseliner-sandbox` org is the acceptance suite: each fixture repo fails exactly the
check its name advertises, and `test-pass` scores 1.00. The Go scan must reproduce the same
per-repo scores and pass/fail verdicts the Python version produces against that org.
