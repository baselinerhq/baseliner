package checks

import (
	"fmt"

	"github.com/baselinerhq/baseliner/internal/models"
)

const staleThresholdDays = 90

type defaultBranchIsMain struct{ base }

func (c defaultBranchIsMain) Eval(r *models.NormalizedRepository) models.CheckResult {
	// Render a nil branch as "None" to match the Python f-string output.
	branch := "None"
	if r.Git.DefaultBranch != nil {
		if *r.Git.DefaultBranch == "main" {
			return c.pass()
		}
		branch = *r.Git.DefaultBranch
	}
	return c.fail(fmt.Sprintf("Default branch is '%s', expected 'main'", branch))
}

type staleRepo struct{ base }

func (c staleRepo) Eval(r *models.NormalizedRepository) models.CheckResult {
	if !r.Git.IsStale {
		return c.pass()
	}
	daysText := "unknown"
	if r.Git.DaysSinceCommit != nil {
		daysText = fmt.Sprintf("%d", *r.Git.DaysSinceCommit)
	}
	return c.fail(fmt.Sprintf(
		"Repository has had no commits in %s days (threshold: %d)", daysText, staleThresholdDays))
}
