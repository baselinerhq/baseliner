# Go vs Python — full parity sweep & triage

An unbiased component-by-component comparison of the Go port against the original
Python implementation, performed before the Go cutover. Each component was reviewed
independently (fresh-eyes) for behavioral parity **and** long-term code quality.

Verdict of the sweep: **no parity *bugs* that change a score/verdict under the shipped
default policy.** Several genuine divergences and robustness gaps were found; the ones
worth fixing long-term are listed under "Accepted" and were implemented before cutover.
The rest are documented as deliberate, low-impact divergences.

## Accepted fixes (implemented)

| # | Component | Finding | Why it's a real long-term fix |
|---|-----------|---------|-------------------------------|
| A | models | `CheckDefinition.Enabled` defaulted to Go's zero value `false`; Python's pydantic default is `True`. Masked only because the embedded `default.yaml` sets `enabled: true` on every check. | A user-authored custom policy that omits `enabled:` would **silently disable** those checks in Go and produce different scores. Correctness. |
| B | output | Integer-valued scores serialized as `1`/`0` (Go `encoding/json`) vs `1.0`/`0.0` (pydantic). Fires on every perfect/zero score; the golden test baked in the Go behavior, masking it. The numeric differential test couldn't catch it (1 == 1.0 numerically). | JSON byte-fidelity; this is the most common output divergence. |
| C | discovery | Include/exclude globs used `path.Match`, which differs from Python `fnmatch`: negated char classes are inverted (`[!…]` vs `[^…]`) and malformed patterns silently never-match. | Produces a **different repo set** silently from the same user config. Glob config is user-facing. |
| D | engine + runner | No per-repo `recover()`. Python wraps each repo in collection (`collection_error`) and evaluation (`engine_error`) with try/except, emitting a single ERROR/critical row and continuing. Go would abort the whole batch on a panic, and `mergeCollectionErrors` was dead code. | Fleet robustness (one bad repo shouldn't kill a 500-repo scan) + restores the exact failure-result shape + removes dead code. |
| E | engine | Scoring used `math.Round` (half-away-from-zero); Python `round()` is banker's rounding (half-to-even). Confirmed divergence at e.g. `17/32` → Go `0.5313`, Python `0.5312`. | Unreachable with the default policy (max total weight 23 < the 32 needed) but reachable with large custom policies. Correctness across arbitrary policies. |
| F | collectors | `filepath.WalkDir` classifies a directory **symlink** as a file (its `IsDir()` is false), adding a phantom path the Python `os.walk` (followlinks=False) never lists. | Removes genuine phantom-file behavior that could (rarely) flip a key-file/CI detection. |
| G | collectors | Default-branch name was cut at the last `/` (`refs/remotes/origin/HEAD` → after last slash), truncating multi-segment branch names; Python strips the fixed prefix. | Correct handling of branch names containing `/`. |
| H | cli | `--format` accepted any value silently (unknown → no output, exit 0/1). Python rejects invalid values with exit 2. | Prevents a silent no-output footgun; matches documented exit codes. |
| I | checks | `readme_has_heading` split on `"\n"` only and trimmed only `" \t"`; Python uses `str.splitlines()` (all line boundaries) and `lstrip()` (all Unicode whitespace). | Robust heading detection across line endings / Unicode indentation. |
| J | output | Atomic JSON write left a stray `.tmp` if `WriteFile` itself failed (only the rename path cleaned up). Python unlinks on any write failure. | Defensive cleanup for real failure modes (disk full, perms). |

## Documented divergences (deliberately not changed)

These cannot change a score/verdict, are untestable across separate runs, or concern
extinct/exotic inputs. Changing them would be churn, not value.

- **JSON `timestamp` sub-second formatting.** Go `RFC3339Nano` trims trailing zeros; pydantic pads to 6 digits. (The often-claimed `+00:00` vs `Z` difference does **not** exist — current pydantic v2 emits `Z` for UTC, matching Go.) Timestamps are different instants between a Go run and a Python run, so byte-equality is impossible regardless; the differential test compares scores+verdicts, not timestamps.
- **Non-UTF-8 README replacement.** Go `ToValidUTF8` collapses each invalid run to one U+FFFD; Python `decode(errors="replace")` emits one per sub-sequence. Content-only; cannot change nonempty/heading results.
- **`~user` home expansion** (other users' homes) — not supported in Go; `~`/`~/` are. Rare.
- **Unexpected-error stderr text** — Go `%T` yields `*errors.errorString` vs Python's class name. Exit code (2) matches; the path is for internal bugs only.
- **`--no-issues` negation flag** — Python has the Typer `--open-issues/--no-issues` pair; Go has only `--open-issues` (default false), so the negation is redundant.
- Minor: rate-limit reset timestamp format in a log line; staleness day-count for future-dated commits (no effect on `is_stale`); console color keyed to stdout vs the writer; `ensureLabel` leniency on transient (non-404) errors.

## Confirmed at parity (no change needed)

All 10 check messages are byte-for-byte identical; severity weights and the
severity-weighted scoring formula match; layer-skip and all-skip→1.0 match; exit codes
0/1/2 and their precedence match; the GitHub issue body markdown (icons, `  \n` breaks,
score `:.0f%`, timestamp format, footer) matches; idempotent issue create/update,
dry-run, label name/color, and the 1.1s rate-limit sleep match; discovery ordering,
pagination, exclude-wins-over-include, and typical glob patterns match; the filesystem
walk (depth-4, `.git` exclusion, sort+dedupe), git default-branch-via-`origin/HEAD`
(with the deliberate no-fallback-to-HEAD), and GitHub API collection paths all match.
