# Validation log

Evidence for the Go port, appended as components land. Every entry is real command output.
See `docs/go-parity-plan.md` §12 for the runbook this follows.

## Decisions (autonomous)

- **`default_branch` nil when no `origin/HEAD`.** The local-git collector matches Python
  exactly (no fallback to the checked-out HEAD, unlike the early prototype). nil renders as
  `'None'` in the `default_branch_is_main` message.
- **JSON `timestamp`**: current pydantic v2 emits `...Z` for UTC, matching Go's RFC3339 —
  so the previously-assumed `+00:00` divergence does **not** exist. The only residual
  difference is sub-second trailing-zero padding, which is moot (a Go run and a Python run
  are different instants, so timestamps never match byte-for-byte regardless). See
  `docs/SWEEP.md`.
- **Validation messages** (config) are clear equivalents of pydantic's internal text; the
  `Error:`/exit-2 contract matches, the detail string does not.

## Evidence

### Milestone A — local-scope parity (Go vs Python, same config, 11 sandbox clones)

`go test ./...`: all packages pass. `go vet ./...`: clean. Console + JSON golden tests pass.

Differential (`/tmp/bl-local.yaml`, both binaries → JSON → slug→(score, verdict)):

```
repo                                  python                go  match
.github                     (0.5217, 'FAIL')  (0.5217, 'FAIL')  OK
test-no-ci                  (0.8696, 'FAIL')  (0.8696, 'FAIL')  OK
test-no-codeowners          (0.9565, 'FAIL')  (0.9565, 'FAIL')  OK
test-no-dep-updates          (0.913, 'FAIL')   (0.913, 'FAIL')  OK
test-no-gitignore            (0.913, 'FAIL')   (0.913, 'FAIL')  OK
test-no-license             (0.8696, 'FAIL')  (0.8696, 'FAIL')  OK
test-no-readme              (0.6087, 'FAIL')  (0.6087, 'FAIL')  OK
test-non-main-branch         (0.913, 'FAIL')   (0.913, 'FAIL')  OK
test-pass                      (1.0, 'PASS')       (1, 'PASS')  OK
test-readme-empty           (0.7826, 'FAIL')  (0.7826, 'FAIL')  OK
test-readme-no-heading       (0.913, 'FAIL')   (0.913, 'FAIL')  OK

TOTAL repos: 11 | mismatches: 0 | DIFFERENTIAL: PASS ✅
```

(Local collection legitimately differs from the GitHub-API path — e.g. `.github` scores
0.52 locally because `profile/README.md` exists on disk — so this compares like-for-like:
Go-local vs Python-local.)

### Milestone B — GitHub-scope parity (Go vs Python, live `baseliner-sandbox` org)

Both binaries scanned the live org via the GitHub API. Exit codes matched (1 / 1).

```
repo                                  python                go  match
.github                     (0.1304, 'FAIL')  (0.1304, 'FAIL')  OK
test-no-ci                  (0.8696, 'FAIL')  (0.8696, 'FAIL')  OK
test-no-codeowners          (0.9565, 'FAIL')  (0.9565, 'FAIL')  OK
test-no-dep-updates          (0.913, 'FAIL')   (0.913, 'FAIL')  OK
test-no-gitignore            (0.913, 'FAIL')   (0.913, 'FAIL')  OK
test-no-license             (0.8696, 'FAIL')  (0.8696, 'FAIL')  OK
test-no-readme              (0.6087, 'FAIL')  (0.6087, 'FAIL')  OK
test-non-main-branch         (0.913, 'FAIL')   (0.913, 'FAIL')  OK
test-pass                      (1.0, 'PASS')       (1, 'PASS')  OK
test-readme-empty           (0.7826, 'FAIL')  (0.7826, 'FAIL')  OK
test-readme-no-heading       (0.913, 'FAIL')   (0.913, 'FAIL')  OK

TOTAL: 11 | mismatches: 0 | DIFFERENTIAL: PASS ✅
```

The same `.github` repo scores 0.13 here (API: no root README) vs 0.52 locally — and the
Go port matches Python in **both** collection modes. GitHub-only parity (Milestone B) proven.

### Quality gates

- `go test -race ./...`: **all 10 packages pass** (41 test functions). Race-clean.
- `golangci-lint run` (v2.12.2): **0 issues**.
- `go vet ./...`: clean. `gofmt`: clean.
- **Go CI workflow green** on `feat/go-port` (build, vet, test -race, golangci-lint) —
  run `27668429597`. Legacy Python CI also green on the PR (Python tree untouched).
- Golden tests lock the console summary, the JSON artifact, and the GitHub issue body.
- `--open-issues --dry-run` against the live org: reads only, logs intent, no writes.
- Exit-code parity: Go and Python both return `1` (failures) on the sandbox org, `0` on an
  all-pass repo, `2` on missing config / no sources / missing `--open-issues` token.
- Concurrency (errgroup, limit 8) leaves output order and scores identical to a serial run.

### Coverage vs the 82 Python tests

Behaviors ported and covered by Go tests: severity weights, policy load, all 10 checks +
layer-skip, engine scoring + ignore/repo-ignore/unknown/disabled/all-skip, file detectors,
filesystem collector (depth/.git/truncation), git collector (origin/HEAD + nil fallback),
config load/validation, discovery globs, console + JSON + issue-body goldens, and CLI exit
codes. The headline gate beyond the Python suite is the **differential acceptance** test
(`scripts/diff-acceptance.sh`), which neither suite had before.

### Post-sweep re-validation (after the 10 parity fixes)

A full unbiased Go-vs-Python sweep (`docs/SWEEP.md`) produced 10 accepted fixes. After
applying them, parity was re-proven:

- `go test -race ./...`: all packages pass (now 50 test functions, incl. new tests for the
  `enabled` default, `Score` 1.0/0.0 marshaling, fnmatch globs, `engine_error`/
  `collection_error` recover, half-to-even rounding, dir-symlink skip, slashed default
  branch, `--format` validation, and CRLF/Unicode heading detection).
- `golangci-lint run` (v2.12.2): **0 issues**. `go vet` / `gofmt`: clean.
- **Differential acceptance — local scope** (10 sandbox clones): `10 repos | 0 mismatches |
  DIFFERENTIAL: PASS`.
- **Differential acceptance — GitHub scope** (live `baseliner-sandbox` org, 11 repos):
  `11 repos | 0 mismatches | DIFFERENTIAL: PASS`.
- **Byte-for-byte JSON parity**: with `run_id` and `timestamp` (the only inherently
  per-run fields) normalized, the Go and Python JSON artifacts for the local scope are
  **identical** — confirming the `score: 1.0`/`0.0` fix closed the last byte divergence.

```
$ diff <(norm py-local.json) <(norm go-local.json)
>>> IDENTICAL — byte-for-byte JSON parity (modulo run_id/timestamp)
```

## Run it yourself

```bash
cd baseliner-go-port
go test -race ./...
golangci-lint run
# parity vs Python (needs uv + GITHUB_TOKEN for github scope):
./scripts/diff-acceptance.sh /path/to/baseliner.yaml ../baseliner
```
