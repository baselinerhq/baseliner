// Package privacy guards against disclosing private/internal repositories when a
// scan's output goes to a public place (e.g. a public control repo's Actions
// logs and artifacts). It transforms the assembled RunResult into a
// disclosure-safe view; the caller keeps the original for issue-opening and the
// exit-code gate, which must always see every repo.
package privacy

import (
	"fmt"

	"github.com/baselinerhq/baseliner/internal/models"
)

// Mode is the treatment applied to private/internal repos in a public context.
type Mode string

const (
	// ModeAllow discloses private repos normally (opt-out / explicit ack).
	ModeAllow Mode = "allow"
	// ModeRedact masks the repo slug to "private/N" and strips finding
	// messages, but keeps the score and per-check statuses in the aggregate.
	ModeRedact Mode = "redact"
	// ModeExclude drops private repos from the public output entirely.
	ModeExclude Mode = "exclude"
	// ModeFail refuses to produce public output if any private repo would be
	// disclosed, forcing an explicit decision.
	ModeFail Mode = "fail"
)

// DefaultMode is used when no mode is configured.
const DefaultMode = ModeRedact

// ParseMode validates and normalizes a configured mode string. An empty string
// yields DefaultMode.
func ParseMode(s string) (Mode, error) {
	switch Mode(s) {
	case "":
		return DefaultMode, nil
	case ModeAllow, ModeRedact, ModeExclude, ModeFail:
		return Mode(s), nil
	default:
		return "", fmt.Errorf("invalid privacy.private_repos %q: must be allow, redact, exclude, or fail", s)
	}
}

// Options configures Apply.
type Options struct {
	// PublicContext is true when the scan output is going somewhere public.
	PublicContext bool
	// Mode is the treatment for private/internal repos when PublicContext.
	Mode Mode
}

// protected reports whether a repo with the given GitHub visibility must be
// guarded. "internal" (enterprise-visible) is protected like "private";
// "public" and unknown/local ("") are disclosed.
func protected(visibility string) bool {
	return visibility == "private" || visibility == "internal"
}

// Apply returns a disclosure-safe copy of run. The input run is never mutated.
//
// vis maps a repo slug to its GitHub visibility ("public"|"private"|"internal");
// a missing slug (local or non-GitHub source) is treated as public. When
// PublicContext is false or Mode is allow, run is returned unchanged.
//
// In ModeFail, if any private/internal repo would be disclosed, Apply returns a
// non-nil error and the caller must not write any public output.
func Apply(run models.RunResult, vis map[string]string, o Options) (models.RunResult, error) {
	if !o.PublicContext || o.Mode == ModeAllow || o.Mode == "" {
		return run, nil
	}

	// Count protected repos up front (drives fail and the note). The count is
	// safe to surface; the slugs are not — the error itself goes to the public
	// log in a public context, so it must never name a private repo.
	protectedCount := 0
	for _, rr := range run.Repos {
		if protected(vis[rr.Slug]) {
			protectedCount++
		}
	}
	if protectedCount == 0 {
		return run, nil
	}

	switch o.Mode {
	case ModeFail:
		return models.RunResult{}, fmt.Errorf(
			"%d private/internal repo(s) would be disclosed in a public context. "+
				"Set privacy.private_repos to redact/exclude/allow, or mark the context private",
			protectedCount)
	case ModeExclude:
		return exclude(run, vis), nil
	case ModeRedact:
		return redact(run, vis), nil
	default:
		// Unreachable: Mode is validated via ParseMode before reaching here.
		return run, nil
	}
}

// redact returns a copy of run with each private/internal repo's slug masked to
// "private/N" and every finding message stripped, preserving scores, statuses,
// and aggregate counts.
func redact(run models.RunResult, vis map[string]string) models.RunResult {
	out := run
	out.Repos = make([]models.RepoResult, len(run.Repos))
	n := 0
	count := 0
	for i, rr := range run.Repos {
		if !protected(vis[rr.Slug]) {
			out.Repos[i] = rr
			continue
		}
		n++
		count++
		masked := rr
		masked.Slug = fmt.Sprintf("private/%d", n)
		// Copy results with messages nulled — a check or collection-error
		// message can embed the real slug or a private path.
		masked.Results = make([]models.CheckResult, len(rr.Results))
		for j, c := range rr.Results {
			c.Message = nil
			masked.Results[j] = c
		}
		out.Repos[i] = masked
	}
	out.Privacy = &models.PrivacyNote{Mode: string(ModeRedact), Count: count}
	return out
}

// exclude returns a copy of run with private/internal repos removed and the
// aggregate counts recomputed for the disclosed subset.
func exclude(run models.RunResult, vis map[string]string) models.RunResult {
	out := run
	out.Repos = make([]models.RepoResult, 0, len(run.Repos))
	count := 0
	passed := 0
	for _, rr := range run.Repos {
		if protected(vis[rr.Slug]) {
			count++
			continue
		}
		out.Repos = append(out.Repos, rr)
		if !hasFailOrError(rr) {
			passed++
		}
	}
	out.TotalRepos = len(out.Repos)
	out.Passed = passed
	out.Failed = len(out.Repos) - passed
	out.Privacy = &models.PrivacyNote{Mode: string(ModeExclude), Count: count}
	return out
}

// hasFailOrError reports whether a repo has any failing or errored check (a repo
// passes only when none do) — mirrors runner.hasFailOrError.
func hasFailOrError(rr models.RepoResult) bool {
	for _, c := range rr.Results {
		if c.Status == models.StatusFail || c.Status == models.StatusError {
			return true
		}
	}
	return false
}
