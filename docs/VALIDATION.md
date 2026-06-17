# Validation log

Evidence for the Go port, appended as components land. Every entry is real command output.
See `docs/go-parity-plan.md` §12 for the runbook this follows.

## Decisions (autonomous)

- **`default_branch` nil when no `origin/HEAD`.** The local-git collector matches Python
  exactly (no fallback to the checked-out HEAD, unlike the early prototype). nil renders as
  `'None'` in the `default_branch_is_main` message.
- **JSON `timestamp`** uses Go RFC3339 (`...Z`) vs pydantic's `+00:00` offset — documented
  divergence (`go-parity-plan.md` §8). Scores/verdicts are unaffected.
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
