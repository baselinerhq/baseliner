# Prototype findings: go-git GitCollector

**Verdict: GO.** go-git fully replicates the Python `GitCollector` with no shell-out,
preserving the single-static-binary goal.

Validated against real clones with known ground truth (today = 2026-06-17):

| Repo | Expected default | go-git result | Match |
|---|---|---|---|
| `test-pass` | main | main | ✅ |
| `test-non-main-branch` | master | master | ✅ |
| `baseliner` | main | main | ✅ |

Edge cases:

- **No remote** (no `origin/HEAD`): falls back to the checked-out HEAD branch name. ✅
- **No commits** (empty repo): returns a graceful error, no panic; batch continues. ✅
- Last-commit time (UTC) and `days_since_commit` / `is_stale` (90-day threshold) correct. ✅
- Branch listing correct. ✅

Resolves Decision #1 in `PORTING.md`: the local-git collector uses **go-git** (pure Go),
not a `git` shell-out, and **stays in scope** rather than being deferred. The prototype
lives in `_prototype/gitcollector/` and is throwaway reference; the production collector
will live in `internal/collectors/`.
