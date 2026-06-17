# Go Parity Plan

This is the authoritative specification for porting `baseliner` from Python to Go
at **full behavioral parity**. It is written against a complete, line-level review
of the Python implementation (`src/baseliner/**`) and its 82 tests (`tests/**`).

`PORTING.md` covers *why* and the high-level phasing. This document covers *exactly
what each component must do* and the subtle things a reimplementation gets wrong.

- Audience: whoever implements the remaining Go components (issues #12–#25).
- Definition of "parity": for any given config + repo state, the Go binary produces
  the same per-repo scores, the same pass/fail verdicts, the same exit code, the same
  console summary (modulo ANSI color codes), the same JSON shape, and the same GitHub
  issue body as the Python tool. Deliberate divergences are listed explicitly in
  §8 and nowhere else.

---

## 1. Status at time of writing

Branch `feat/go-port`. Module `github.com/baselinerhq/baseliner`. Layout: `cmd/baseliner`,
`internal/...`. The Python tree still lives at the repo root and remains the shipping
implementation until cutover.

| Area | Python source | Go status | Issue |
|---|---|---|---|
| Data models | `models/*.py` | **done** (needs fidelity fixes — §8) | #11 |
| Severity + weights | `models/policy.py` | **done** | #11 |
| Embedded default policy | `policies/default.yaml` | **done** (`//go:embed`) | #10 |
| Policy loader | `config.py:PolicyLoader` | **done** | #12 |
| Config loader + schema | `config.py` | **not started** | #12 |
| Checks (10) + registry + layer-skip | `checks/*.py` | **done** (tested) | #13 |
| Policy engine + scoring | `engine.py` | **done** (tested; score parity verified) | #17 |
| Filesystem collector | `collectors/filesystem.py` | **not started** | #14 |
| GitHub-API collector | `collectors/github_api.py` | **not started** | #15 |
| Local-git collector | `collectors/git.py` | **prototype validated** | #21 |
| Discovery (local + github) | `discovery/*.py` | **not started** | #16 |
| Console output | `output/console.py` | **not started** | #18 |
| JSON output | `output/json.py` | **not started** | #18 |
| CLI orchestration + exit codes | `cli.py` | **skeleton only** | #19 |
| GitHub issues action | `actions/github_issues.py` | **not started** | #20 |
| Concurrency fan-out | (new) | **not started** | #22 |
| Test suite port | `tests/**` | **partial** (models/policy/checks/engine) | #23 |
| Distribution | (new) | **not started** | #24/#25 |

Verified so far: the engine scores a no-README repo at `0.6087`, matching the Python
scan's `0.61` (2-dp display) against `baseliner-sandbox/test-no-readme`.

---

## 2. Dependency map (authoritative)

| Concern | Python | Go |
|---|---|---|
| CLI | `typer` | `github.com/spf13/cobra` |
| Models/validation | `pydantic` | plain structs + explicit validation |
| YAML | `pyyaml` (`yaml.safe_load`) | `gopkg.in/yaml.v3` |
| GitHub API | `pygithub` | `github.com/google/go-github/v66` + `golang.org/x/oauth2` |
| Local git | `gitpython` | `github.com/go-git/go-git/v5` *(prototype validated)* |
| Embedded policy | `importlib.resources` | `//go:embed` |
| Console color | `typer.style` (click) | `github.com/fatih/color` (auto-disables off-TTY) |
| Logging | `logging` | `log/slog` |
| UUID | `uuid.uuid4()` | `crypto/rand` helper (already in engine) |

go-github major version pinned at implementation time; `v66` is a placeholder.

---

## 3. The pipeline (must preserve order and semantics)

From `cli.py:117-241`. The `scan` command does, in order:

1. **Configure logging** from `--verbose`/`--quiet` (§7.4).
2. **Load config** (`load_config`) → **load policy** (`PolicyLoader.load(cfg.policy.base)`)
   → build check registry → construct engine with `global_ignore=cfg.policy.ignore`,
   `repo_ignores=cfg.policy.repo_ignores`.
3. **Discover** sources: if `scope.github` set, run `GitHubDiscovery`; if `scope.local.paths`
   non-empty, run `LocalDiscovery`. Concatenate (github first, then local).
4. If **no sources**: print `No repositories discovered. Check your scope config.` to
   **stderr** and exit **2**.
5. **Collect** each source into a `NormalizedRepository`:
   - `github` source → `GitHubAPICollector.collect` (returns fs **and** git context).
   - local source → `FilesystemCollector.collect`; then `GitCollector.collect`; if the
     latter returns non-nil, **merge its git context** into the fs repo
     (`repo.model_copy(update={"git": ...})`).
   - A collection exception is caught per-source and recorded as a synthetic
     `RepoResult` with one `collection_error` check (status `error`, severity `critical`,
     message = `str(exc)`). These are held aside in `repo_error_results`.
6. **Evaluate**: `engine.run_batch(repos)` → `RunResult`.
7. If there were `collection_error` results, **append** them to `run_result.repos` and
   **recompute** `total_repos`/`passed`/`failed` over the combined list (`cli.py:185-203`).
8. **Output**: if format ∈ {json, both} → `write_json(run_result, output_file)`. If format
   ∈ {table, both} **and not** `--quiet` → `print_summary`.
9. **Issues** (if `--open-issues`): resolve token from `scope.github.token_env` (default
   `GITHUB_TOKEN`); empty/whitespace → stderr `--open-issues requires a GitHub token in
   '{env}'`, exit **2**. Build `source_map` by slug; for each `run_result.repos` entry with
   a GitHub source reference, run `GitHubIssueAction`. Per-repo failures are logged (warn),
   not fatal.
10. **Exit code**: if `run_result.failed > 0` → exit **1**; else exit **0**. Error classes
    map to exit **2** (§7.5).

> Note the ordering subtlety in step 7: collection errors are merged **after** `run_batch`,
> and pass/fail is recomputed over the union. The Go port must do the same, or counts drift
> when a repo fails to collect.

---

## 4. Component specifications

For each: the Go target, exact behavior, edge cases, and which Python tests pin it.

### 4.1 Config + schema — `internal/config` (#12)

Go structs mirroring `config.py`:

```
type PolicyConfig struct { Base string; Ignore []string; RepoIgnores map[string][]string }
type GitHubScope  struct { Type string; Name string; TokenEnv string }   // Type ∈ {org,user}
type LocalScope   struct { Paths []string }
type Scope        struct { GitHub *GitHubScope; Local *LocalScope; Include []string; Exclude []string }
type Config       struct { Scope Scope; Policy PolicyConfig }
```

Defaults (applied when key absent): `Policy.Base="default"`, `Ignore=[]`, `RepoIgnores={}`,
`TokenEnv="GITHUB_TOKEN"`, `Include=[]`, `Exclude=[]`, `GitHub=nil`, `Local=nil`.

`LoadConfig(path)`:
- File missing → `ConfigError("Config file not found: {path}")`.
- YAML parse error → `ConfigError("Invalid YAML in config file: {err}")`.
- `nil`/empty document → treat as `{}` (then validation runs).
- Validation: `Scope` is **required**; `GitHubScope.Type` must be `org` or `user` and
  `Name` non-empty when `github:` present. Failure → `ConfigError("Config validation failed: {detail}")`.

Because there is no pydantic, validation is an explicit function. Apply defaults by
initializing struct fields before `yaml.Unmarshal` (yaml.v3 leaves untouched fields at
their zero value, so set `TokenEnv`/`Base` defaults post-unmarshal if empty, and
distinguish "absent `github:`" via a `*GitHubScope` pointer that stays nil).

Pins: `tests/unit/test_config.py` (defaults, missing file, invalid YAML, validation).

### 4.2 Checks — `internal/checks` (#13) — DONE

Already implemented and tested. Exact fail messages (must not change):

| check | layer | fail message |
|---|---|---|
| `readme_exists` | fs | `No README file found` |
| `readme_nonempty` | fs | `README not found` (no README) / `README is present but empty` |
| `readme_has_heading` | fs | `README not found` (no README) / `README has no headings (expected at least one # heading or underline heading)` |
| `license_exists` | fs | `No LICENSE or COPYING file found` |
| `gitignore_exists` | fs | `No .gitignore found` |
| `ci_present` | fs | `No CI workflow files found` |
| `codeowners_exists` | fs | `No CODEOWNERS file found` |
| `dependency_update_config` | fs | `No Dependabot or Renovate config found` |
| `default_branch_is_main` | git | `Default branch is '{actual}', expected 'main'` |
| `stale_repo` | git | `Repository has had no commits in {days|unknown} days (threshold: 90)` |

Heading detection: markdown = any line whose `lstrip()` starts with `#`; underline = a
non-empty line followed by a non-empty line of length ≥3 that is all `=` or all `-`.
Layer-skip messages: `Filesystem context not available` / `Git context not available` /
`Platform context not available`, status `skip`. Pins: `test_checks_hygiene.py`,
`test_checks_git.py`.

### 4.3 Engine — `internal/engine` (#17) — DONE

Implemented and tested. Contract: enabled checks only; skip global/repo-ignored; unknown
check id → warn + skip; **policy severity overrides** the check's result severity; score =
`round(passed_weight/total_weight, 4)`, skips excluded, all-skip ⇒ `1.0`. `RunBatch`: a repo
is *passed* iff **no** result is `fail` or `error` (all-skip counts as passed); `failed =
total - passed`; `run_id` = uuid4 string. The Python `run_batch` also wraps per-repo eval in
try/except producing an `engine_error` result (critical/error) — port this guard too.
Pins: `test_policy_engine.py`.

### 4.4 Filesystem collector — `internal/collectors` (#14)

Build a `FilesystemContext` from a local directory. Detection tables (all lowercase
comparisons unless noted):

- README filenames: `readme.md`, `readme.rst`, `readme.txt`, `readme`.
- LICENSE filenames: `license`, `license.md`, `license.txt`, `copying`.
- GITIGNORE: `.gitignore`.
- CODEOWNERS: filename == `codeowners` **and** parent dir ∈ {`.`, ``, `.github`}.
- CI files: path starts with `.github/workflows/` and ends `.yml`/`.yaml`; or `==
  .circleci/config.yml`; or filename == `jenkinsfile`; or `== .gitlab-ci.yml`. Dedup + sort.
- Dep-update files: exact lowercased relative path ∈ {`.github/dependabot.yml`,
  `.github/dependabot.yaml`, `renovate.json`, `.renovaterc`, `.renovaterc.json`}. Dedup + sort.

Walk: top-down; **prune `.git`**; compute depth = number of path components of the dir
relative to root (root = 0); when depth ≥ 4, stop descending; skip files whose relative
path has > 4 components. Collect relative POSIX paths, **dedup + sort**.

README content: locate the first file matching README filenames; read **first 4096 bytes**,
then decode UTF-8 replacing invalid bytes. `nil` if no README or unreadable.

`name` = base name of the resolved path. If the path is missing/not-a-dir, return an
"empty" context (all key files false, empty lists, nil readme) — **do not** error.

Edge: README byte-truncation is on *bytes* not runes; a multibyte char split at 4096 is
replaced. Go: slice the byte buffer to 4096 then `strings.ToValidUTF8(string(b), "�")`
(Go's UTF-8 decoding differs subtly from Python's `errors="replace"`; treat exact
replacement-char placement as out of scope — see §8). Pins: `test_filesystem_collector.py`.

### 4.5 Local-git collector — `internal/collectors` (#21)

Prototype already validated (`_prototype/FINDINGS.md`). Productionize into the package.
Require a `.git` directory; else return `nil` (caller skips git context → git checks skip).
- `default_branch`: symbolic ref `refs/remotes/origin/HEAD` → last path segment; fall back
  to checked-out HEAD short name; `nil`/None if neither.
- `last_commit_at`: HEAD commit committer time → UTC.
- `days_since_commit`: `int((now_utc - last_commit_at).hours / 24)` (floor), matching
  Python's `timedelta.days`.
- `is_stale`: `days_since_commit > 90`.
- `branches`: local branch short names.
- On any go-git error (e.g. no commits): return `nil` (Python returns None and the repo
  keeps only fs context). Pins: `test_git_collector.py`.

### 4.6 GitHub-API collector — `internal/collectors` (#15)

Uses go-github. **Shallow** by design — only these content paths are listed: `""`,
`.github`, `.github/workflows`, `.circleci`. Combine file paths (type == file), dedup+sort,
then reuse the *same* `detect_key_files`/`detect_ci_files`/`detect_dependency_update_files`
helpers as the filesystem collector (factor them into a shared internal func).

- `get_contents(path)`: 404 → empty list (not an error); other API error → warn + empty.
- `readme_content`: via the repo README endpoint (GitHub's own README detection), first
  4096 bytes, UTF-8 replace; `nil` on not-found/other error.
- `default_branch`: repo's `default_branch` field (nil if absent).
- `last_commit_at`: repo's **`pushed_at`** (not a commit lookup), normalized to UTC; if nil,
  `days_since_commit=nil`, `is_stale=false`.
- `branches`: list branches, **cap at 100**.
- `name`: repo name; fallbacks to path base / slug tail.
- nil `pygithub_repo` analog → return a fully-empty fs+git context (don't error).

Pins: `test_github_api_collector.py`. Note the asymmetry vs filesystem: GitHub README
presence in `key_files["README"]` comes from the *contents listing*, while `readme_content`
comes from the README endpoint — keep both.

### 4.7 Discovery — `internal/discovery` (#16)

`LocalDiscovery(paths)`: expand `~` and resolve to absolute; skip (warn) paths that don't
exist or aren't directories; `RepoSource{Type:"local", Slug: <abs path string>, Path: ...}`.
**Slug is the absolute resolved path** — this is the key used by `repo_ignores` for local
repos.

`GitHubDiscovery(cfg, include, exclude)`:
- Token from `cfg.TokenEnv` (trimmed); empty → `AuthError("GitHub token not found in
  environment variable '{env}'. Set it in your environment and re-run the scan.")`.
- Rate-limit pre-check: `remaining == 0` → `RateLimitError("Rate limit exceeded. Resets at
  {reset iso}. Try again later.")`; `remaining < 100` → warn; any other failure to read the
  limit → debug-log and continue.
- `type==org` → list org repos; else → list user repos (all types).
- Filter: excluded if any `exclude` glob matches the **repo name**; included if `include`
  empty **or** any `include` glob matches. Exclude takes precedence. Use `path.Match`-style
  globbing equivalent to Python `fnmatch` (note: `fnmatch` is case-normalizing on some
  platforms but effectively case-sensitive here; match semantics: `*`, `?`, `[seq]`,
  `[!seq]`). `Slug = "{cfg.Name}/{repo name}"`.

Pins: `test_local_discovery.py`, `test_github_discovery.py`.

### 4.8 Output — `internal/output` (#18)

**Console** (`print_summary` = table + failures + footer):
- Header: `fmt.Sprintf("%-40s  %5s  %5s  %5s  %5s", "repo","score","pass","fail","skip")`.
- Separator: 68 dashes.
- Row per repo: slug truncated to 40, left-justified width 40; score `%5.2f` colored
  (green ≥0.8, yellow ≥0.5, red <0.5); then pass/fail/skip counts right-justified width 5.
  `pass`=count of `pass`; `fail`=count of `fail`+`error`; `skip`=count of `skip`.
- Failures block (only if at least one qualifying failure across all repos): blank line,
  `Critical/high failures:`, then per repo with ≥1 `fail`/`error` whose severity ∈
  {critical, high}: `  {slug}` and `    [{SEV_UPPER}] {check_id}: {message|"(no message)"}`.
  Severity color red for critical, yellow for high.
- Footer: blank line, `{total} repos scanned — {passed} passed, {failed} failed` with the
  failed number colored (green if 0 else red).
- Color must auto-disable when stdout is not a TTY (click does this; `fatih/color` does too).

**JSON** (`write_json`): serialize `RunResult` with 2-space indent. `nil` path → write to
stdout **with a trailing newline**. Path given → atomic write: write `{path}.tmp`, then
rename over `{path}`; on error remove the temp file. Field names and nesting must match
§5. Pins: `test_output_console.py`, `test_output_json.py`.

### 4.9 GitHub issues action — `internal/actions` (#20)

Constants: title `[baseliner] baseline compliance findings`; label name `baseliner`,
color `0075ca`, description `baseliner findings`.

`Run(repoResult, repo)`:
1. Ensure label: get label `baseliner`; if missing and not dry-run, create it; dry-run or
   create-failure → proceed with no label (warn on failure).
2. Build body (below).
3. Find existing **open** issue with label `baseliner` whose **title** equals the constant.
4. Exists: dry-run → log intent and return; else `edit(body)`. Not exists: dry-run → log and
   return; else `create_issue(title, body, labels=[label?])`.
5. Non-dry-run: `sleep(1.1s)` after the write.

Body (exact):
```
## baseliner findings

**Score**: {score*100:.0f}%  
**Scanned**: {YYYY-MM-DD HH:MM UTC}

| check | status | severity | message |
|---|---|---|---|
| `{check_id}` | {icon} {status} | {severity} | {message} |
...

---
*managed by [baseliner](https://github.com/baselinerhq/baseliner)*
```
Note the two trailing spaces after the score line (markdown hard break). Icons: pass `✅`,
fail `❌`, skip `⏭️`, error `⚠️`. Pins: `test_github_issues.py` (idempotency, dry-run,
label create, body).

### 4.10 CLI — `cmd/baseliner` (#19)

Wire §3. Flags already stubbed. Remaining: implement the pipeline, the two stderr+exit-2
guards (no sources; open-issues without token), the error→exit-code mapping (§7.5), and the
combined error-result recompute (step 7). `--version` prints the bare version and exits 0.
Pins: `test_cli_exit_codes.py`, `test_cli_open_issues.py`.

---

## 5. JSON serialization fidelity (read this before #18)

The JSON artifact is a public output (CI uploads `results.json`); shape must match.

`RunResult` → keys in this order: `run_id, timestamp, total_repos, passed, failed, repos`.
`RepoResult`: `slug, timestamp, score, results`. `CheckResult`: `check_id, status, severity,
message`. Go `encoding/json` emits struct fields in declaration order, so order the structs
accordingly (already done).

Three fidelity rules the current Go structs get wrong and must fix under #18/#11:
1. **`message` must serialize as `null`, not be omitted, when absent.** Python emits
   `"message": null`. Change `CheckResult.Message` to `*string` **without** `omitempty`.
2. **`score` is a 4-dp float.** `0.6087` etc. Go marshals `float64` fine; ensure the value
   stored is the rounded one (engine already rounds).
3. **`timestamp` format.** Pydantic v2 emits ISO-8601 with explicit offset
   (`2026-06-17T04:53:48.123456+00:00`). Go `time.Time` marshals RFC3339 with `Z`. This is a
   **documented divergence** (§8) unless we add a custom marshaler; downstream JSON consumers
   parse both. Decide at #18.

Indent is two spaces (`json.MarshalIndent(v, "", "  ")`). stdout output gets a trailing `\n`.

---

## 6. Optional/None semantics (pointers)

Python uses `Optional`/`None` in several places that the Go port must represent as pointers
to preserve both logic and JSON:

- `FilesystemContext.readme_content: str | None` → `*string` (done). nil ⇒ "no README".
- `GitContext.default_branch: str | None` → **should be `*string`** (currently `string`).
  Affects the `default_branch_is_main` message when nil (Python renders `'None'`).
- `GitContext.last_commit_at: datetime | None` → `*time.Time` (done).
- `GitContext.days_since_commit: int | None` → `*int` (done). nil renders `unknown` in the
  stale message.
- `CheckResult.message: str | None` → `*string` (see §5).

Track the `default_branch` and `message` pointer changes as fidelity fixes under #11.

---

## 7. Cross-cutting behavior

### 7.1 Time / UTC
All timestamps normalized to UTC. `days_since_commit` uses floored whole days
(`timedelta.days`). Use a single injectable "now" (the engine already takes `now`) so tests
are deterministic; collectors should also accept an explicit `now` for testability.

### 7.2 Globbing
`fnmatch` semantics for include/exclude on repo **name** only. Go's `path.Match` is close
but errors on malformed patterns and treats `/` specially; repo names have no `/`, so
`path.Match` is acceptable, or vendor a tiny fnmatch. Empty include ⇒ include-all.

### 7.3 GitHub client differences (go-github vs pygithub)
- pagination: pygithub auto-paginates; go-github returns pages — iterate `ListOptions`.
  Honor the **100-branch cap** explicitly.
- contents 404: pygithub raises `GithubException(status=404)`; go-github returns an
  `*ErrorResponse` with `Response.StatusCode==404`. Map both to "empty".
- README: pygithub `get_readme()` → go-github `Repositories.GetReadme`.
- rate limit: pygithub `get_rate_limit().core`; go-github `RateLimits` / `Response.Rate`.
- staleness source is `pushed_at` (a repo field), **not** a commits API call — cheap.

### 7.4 Logging
`log/slog` with levels: default INFO, `--verbose` DEBUG (with source/tracebacks analog),
`--quiet` WARNING. `--verbose` + `--quiet` ⇒ verbose wins, and emit the debug line
`Both --verbose and --quiet given; --verbose wins`. Logs go to stderr; never to the JSON
stdout stream.

### 7.5 Errors → exit codes
Define typed errors `ConfigError`, `AuthError`, `RateLimitError` (sentinel wrapping or typed
structs). Mapping at the top of `scan`:

| Condition | stderr | exit |
|---|---|---|
| all repos passed | — | 0 |
| ≥1 repo failed/errored | — | 1 |
| no sources discovered | `No repositories discovered. Check your scope config.` | 2 |
| `--open-issues` w/o token | `--open-issues requires a GitHub token in '{env}'` | 2 |
| ConfigError | `Error: {msg}` | 2 |
| AuthError | `Auth error: {msg}` | 2 |
| RateLimitError | `{msg}` | 2 |
| any other error | `Unexpected error: {ErrType}: {msg}` | 2 |

`--verbose` adds the stack/debug context for the generic case.

### 7.6 Color off-TTY
Console color must vanish when output is redirected (so golden-file tests and CI logs are
clean). `fatih/color` checks `isatty` and `NO_COLOR`; verify in the table tests by capturing
to a buffer (no codes expected).

---

## 8. Deliberate divergences (the only allowed ones)

1. **JSON `timestamp` format**: RFC3339 `Z` vs pydantic's `+00:00` offset (see §5). Revisit
   at #18 if a consumer needs byte-identical output; default is RFC3339.
2. **README replacement-char placement**: Go's UTF-8 invalid-byte handling differs from
   CPython's `errors="replace"` at the exact boundary of a 4096-byte cut through a multibyte
   rune. Content equality holds for valid UTF-8; only a torn final rune can differ. Accepted.
3. **`default_branch` nil rendering**: if we keep `*string`, render nil as `None` in the
   failure message to match Python, or as empty — decide at #15/#21 and note in the test.

Everything else is parity, not a choice.

---

## 9. Testing strategy (#23)

1. **Port unit tests table-driven**, one Go test file per Python test file. Fixtures
   (`full_repo`, `bare_repo`, `no_git_repo`) move to `testdata/` (note: `bare_repo` needs a
   real `.git`; create it in test setup or check in a `.git` dir as `testdata`). Done so far:
   models, policy loader, checks, engine.
2. **Golden files** for the two public outputs: capture console summary (color disabled) and
   the JSON artifact for a fixed `RunResult`, compare byte-for-byte. This locks §4.8/§5.
3. **Issue-body golden**: render `_build_body` for a known `RepoResult`, compare to a
   checked-in golden (time injected).
4. **Differential acceptance against Python**: run *both* binaries against the
   `baseliner-sandbox` org and assert identical per-repo `score` and pass/fail across all 11
   repos. This is the headline parity gate and should run in CI (or a make target) until
   cutover. The sandbox fixtures already exercise every check (`test-no-*`, `test-pass`).
5. **Exit-code tests**: drive `cmd/baseliner` via `cobra`'s `Execute` with `os.Args`-style
   inputs and assert code + stderr text for each row of §7.5.
6. **CI**: a Go workflow running `go build`, `go vet`, `golangci-lint`, `go test ./...`, and
   (gated by a token) the differential acceptance step.

Target: reproduce all 82 Python test behaviors. Track count parity in the #23 description.

---

## 10. Sequenced work plan

Dependencies first; each step ends green (`go build/vet/test`). Issues in parens.

**Phase 1 — to a working GitHub-only `scan`**
1. Fidelity fixes to models: `Message *string`, `GitContext.DefaultBranch *string` (#11).
2. Config loader + schema + validation (#12).
3. Filesystem collector + shared detection helpers (#14).
4. Local-git collector from the prototype (#21 — pulled early; it unblocks local scans).
5. Local discovery (#16, local half).
6. Output: console + JSON with golden tests (#18).
7. Wire CLI for **local** scope end-to-end; validate against the local sandbox clones (#19).
8. GitHub-API collector (#15) + GitHub discovery (#16, github half).
9. Wire GitHub scope; run the **differential acceptance** vs Python on `baseliner-sandbox`.

> Milestone A (after step 7): `baseliner scan` works on local paths and reproduces Python
> scores on the cloned sandbox repos. Milestone B (after step 9): full GitHub-only parity.

**Phase 2 — feature parity**
10. GitHub issues action + idempotency/dry-run/body golden (#20).
11. Concurrency fan-out across repos, bounded, deterministic output order (#22).
12. Complete the test-suite port; wire differential acceptance into CI (#23).

**Phase 3 — distribution**
13. goreleaser, Homebrew tap, install script, download-only GitHub Action (#24).
14. Docs rewrite around the static binary; deprecate the Python install path (#25).

### Definition of done per phase
- **Phase 1**: GitHub-only `scan` reproduces Python per-repo scores and verdicts on
  `baseliner-sandbox`; exit codes match §7.5; JSON shape matches §5; console matches §4.8.
- **Phase 2**: `--open-issues` idempotent and dry-run-correct; concurrency on by default with
  unchanged output; ≥ the 82 Python test behaviors covered.
- **Phase 3**: one-command install on Linux/macOS/Windows; the control-repo Action uses the
  binary with no Python/uv; docs updated; Python tree removed at cutover.

---

## 11. Risk register

| Risk | Impact | Mitigation |
|---|---|---|
| go-github API shape vs pygithub (404, pagination, README, rate limit) | medium | §7.3; cover each in #15 unit tests with a fake transport |
| JSON datetime/null divergence breaks a downstream consumer | low | §5/§8; add custom marshaler only if a consumer needs it |
| `bare_repo`/git fixtures awkward to vendor | low | construct `.git` in test setup, or use `go-git` to init |
| fnmatch vs path.Match corner cases | low | repo names have no `/`; add a few glob tests |
| Color codes leaking into golden tests | low | §7.6; assert no ANSI when capturing to a buffer |
| Scope creep during cutover (Python + Go coexisting) | medium | keep Python shipping until Milestone B; cut over only when differential passes |

---

## 12. Autonomous execution runbook

This section makes the plan executable end-to-end without a human in the loop. When running
unattended, follow it literally.

### 12.1 Authorization & guardrails (hard rules)

- **Work only on branch `feat/go-port`.** Never commit or push to `main`. Never force-push.
- **Keep PR #27 a draft.** Do **not** merge it.
- **Do not perform the cutover** — do not delete or move the Python tree (`src/`, `tests/`,
  `pyproject.toml`, `uv.lock`), and do not change the Python CI. Cutover is a human decision
  taken after review. The Go code lives alongside Python until then.
- **No other external writes.** Allowed GitHub writes: commits/pushes to `feat/go-port`,
  updating PR #27 body, ticking checkboxes on epic #26, and commenting on the port issues.
  Do **not** open new issues, change repo settings, or touch the sandbox org. The differential
  test only **reads** the sandbox org.
- **Never claim a test/build passed without showing real command output.** If something fails,
  record the failure; do not paper over it.
- **Secrets**: use `gh auth token` for the differential test's `GITHUB_TOKEN`. Never print the
  token or write it to a file.

### 12.2 Per-component loop

Execute §10 steps in order. For each step:

1. Implement the component to the §4 spec (and §5/§6/§7 cross-cutting rules).
2. `gofmt -w` the changed files.
3. `go vet ./...` → `go build ./...` → `go test ./...` — all must pass.
4. Add/port the tests that pin the component (§9). A component is not "done" without tests.
5. Commit (conventional message, `Refs #<issue>`), then `git push`.
6. Tick the component's box on epic #26 and add a one-line progress note if material.
7. Only then move to the next step. If a step fails its gate, fix it before proceeding;
   do not accumulate broken state.

### 12.3 Definition of "meets and exceeds legacy"

**Meets** = strict behavioral parity (this whole document). The master proof is the
differential acceptance test (§9.4): the Go binary and the Python binary produce identical
per-repo `score` and pass/fail verdicts for every repo in `baseliner-sandbox`, plus matching
exit codes and JSON shape.

**Exceeds** = additive improvements that do **not** change observable parity outputs:
- Concurrency: bounded parallel collection+evaluation (#22), default-on, identical output.
- Distribution: single static binary, multi-platform (#24) — the core rationale.
- Robustness: `context.Context` with timeouts on all GitHub calls; typed errors; graceful
  cancellation on SIGINT.
- Quality bar above legacy: `golangci-lint` clean; **golden-file** tests for console/JSON/issue
  body and a **differential** test (neither exists in the Python suite); race detector clean
  (`go test -race`).
- `--version` reports version + commit + build date (via ldflags).
Anything that would alter a parity output (table text, JSON keys, messages, exit codes) is
**out of scope** unless added strictly behind a new flag that defaults to legacy behavior.

### 12.4 Evidence & validation

- Maintain `docs/VALIDATION.md` on the branch. After each milestone, append the **actual**
  command and its output: `go test ./...`, `go test -race ./...`, `golangci-lint run`, and
  the differential test result (the side-by-side score table for all 11 sandbox repos).
- After Milestone B and after Phase 2, post a PR comment on #27 summarizing what landed with
  the key evidence (test counts, differential PASS), so the morning review is a glance.
- The differential test is the gate that flips the PR from "in progress" to "parity proven."
  Implement it as `scripts/diff-acceptance.sh` (runs `uv run baseliner scan` and the Go
  binary against the same config, normalizes JSON, diffs `slug→score` and `slug→verdict`).

### 12.5 Blocker protocol (no human available)

If blocked, do **not** stall silently and do **not** fake progress:
- **Transient** (network flake, rate limit): retry with backoff a few times, then move to an
  unblocked step and return later.
- **Missing capability** (e.g., `golangci-lint` not installed): install it if possible
  (`go install`); if not, record the gap in `VALIDATION.md`, keep `go vet`+tests as the gate,
  and proceed.
- **Genuine ambiguity not covered here**: pick the option that preserves strict parity, record
  the decision and rationale in `VALIDATION.md` under "Decisions", and continue. Never block
  the whole migration on one detail — isolate it.
- **Differential mismatch**: treat as a real bug in the Go port. Fix the Go side to match
  Python (Python is the reference). If the mismatch is a known §8 divergence, normalize it out
  of the comparison and note it.

### 12.6 Stop / done conditions

Stop and leave the PR ready for review when **either**:
- **Done**: Phases 1–2 complete — differential acceptance PASSES on all 11 sandbox repos,
  `go test ./...` and `go test -race ./...` green, `golangci-lint` clean, all §4 components
  implemented with tests, `VALIDATION.md` populated, PR #27 description updated with a status
  summary and evidence links. (Phase 3 distribution is desirable but secondary; do it if time
  remains, else leave #24/#25 open with the rest proven.)
- **Hard-blocked**: a blocker in §12.5 cannot be resolved autonomously. Leave the branch
  building+green at the last good component, write the blocker and the proposed resolution to
  `VALIDATION.md` and a PR comment, and stop.

Under no circumstances mark the work "complete" unless the differential acceptance test has
actually run and passed with its output recorded.

### 12.7 Execution order checklist (tick as completed)

- [ ] §10.1 model fidelity fixes (#11)
- [ ] §10.2 config loader + validation (#12)
- [ ] §10.3 filesystem collector + shared detectors (#14)
- [ ] §10.4 local-git collector productionized (#21)
- [ ] §10.5 local discovery (#16)
- [ ] §10.6 output console + JSON + golden tests (#18)
- [ ] §10.7 wire CLI local scope; validate on local sandbox clones → **Milestone A**
- [ ] §10.8 GitHub-API collector (#15) + GitHub discovery (#16)
- [ ] §10.9 wire GitHub scope; **differential acceptance** vs Python → **Milestone B**
- [ ] §10.10 GitHub issues action (#20)
- [ ] §10.11 concurrency fan-out (#22)
- [ ] §10.12 finish test port + CI workflow + `-race` (#23)
- [ ] Phase 3 distribution if time remains (#24/#25)
- [ ] `VALIDATION.md` populated; PR #27 updated; epic #26 ticked
