package checks

import (
	"fmt"

	"github.com/baselinerhq/baseliner/internal/models"
)

const staleThresholdDays = 90

type defaultBranchIsMain struct{ base }

func (c defaultBranchIsMain) Eval(r *models.NormalizedRepository) models.CheckResult {
	if r.Git.DefaultBranch == nil {
		// A local checkout without refs/remotes/origin/HEAD does not record
		// its default branch.
		return unobservable(c.id, "Default branch unknown: refs/remotes/origin/HEAD is not set "+
			"(with an origin remote, `git remote set-head origin --auto` records it)")
	}
	if *r.Git.DefaultBranch == "main" {
		return c.pass()
	}
	return c.fail(fmt.Sprintf("Default branch is '%s', expected 'main'", *r.Git.DefaultBranch))
}

type staleRepo struct{ base }

func (c staleRepo) Eval(r *models.NormalizedRepository) models.CheckResult {
	if !r.Git.IsStale {
		// Not stale only means something when the last commit was read; a
		// forge that reports no commit time leaves it unknown.
		if r.Git.LastCommitAt == nil {
			return unobservable(c.id, "Last commit time not available")
		}
		return c.pass()
	}
	daysText := "unknown"
	if r.Git.DaysSinceCommit != nil {
		daysText = fmt.Sprintf("%d", *r.Git.DaysSinceCommit)
	}
	return c.fail(fmt.Sprintf(
		"Repository has had no commits in %s days (threshold: %d)", daysText, staleThresholdDays))
}
