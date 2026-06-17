package engine

import (
	"testing"
	"time"

	"github.com/baselinerhq/baseliner/internal/checks"
	"github.com/baselinerhq/baseliner/internal/models"
)

func TestRepoIgnore(t *testing.T) {
	e := New(defaultPolicy(), checks.BuildDefault(), nil, map[string][]string{
		"ig": {"codeowners_exists"},
	})
	rr := e.Run(passingRepo("ig"), time.Unix(0, 0).UTC())
	for _, r := range rr.Results {
		if r.CheckID == "codeowners_exists" {
			t.Error("repo-ignored check should not appear")
		}
	}
}

func TestUnknownCheckSkipped(t *testing.T) {
	pol := defaultPolicy()
	pol.Checks = append(pol.Checks, models.CheckDefinition{ID: "bogus_check", Severity: models.SeverityHigh, Enabled: true})
	e := New(pol, checks.BuildDefault(), nil, nil)
	rr := e.Run(passingRepo("u"), time.Unix(0, 0).UTC())
	for _, r := range rr.Results {
		if r.CheckID == "bogus_check" {
			t.Error("unknown check should be skipped, not in results")
		}
	}
	if rr.Score != 1.0 {
		t.Errorf("score = %v, want 1.0 (unknown check ignored)", rr.Score)
	}
}

func TestAllSkipScoresOne(t *testing.T) {
	// Policy of only git checks, repo with no git context -> both skip -> score 1.0.
	pol := &models.Policy{ID: "git-only", Checks: []models.CheckDefinition{
		{ID: "default_branch_is_main", Severity: models.SeverityMedium, Enabled: true},
		{ID: "stale_repo", Severity: models.SeverityLow, Enabled: true},
	}}
	repo := passingRepo("s")
	repo.Git = nil
	rr := New(pol, checks.BuildDefault(), nil, nil).Run(repo, time.Unix(0, 0).UTC())
	if rr.Score != 1.0 {
		t.Errorf("all-skip score = %v, want 1.0", rr.Score)
	}
	for _, r := range rr.Results {
		if r.Status != models.StatusSkip {
			t.Errorf("expected skip, got %s for %s", r.Status, r.CheckID)
		}
	}
	// A repo whose checks all skip counts as passed.
	run := New(pol, checks.BuildDefault(), nil, nil).RunBatch([]*models.NormalizedRepository{repo}, time.Unix(0, 0).UTC())
	if run.Passed != 1 || run.Failed != 0 {
		t.Errorf("all-skip repo should pass: passed=%d failed=%d", run.Passed, run.Failed)
	}
}

func TestDisabledCheckSkipped(t *testing.T) {
	pol := defaultPolicy()
	pol.Checks[6].Enabled = false // codeowners_exists
	e := New(pol, checks.BuildDefault(), nil, nil)
	rr := e.Run(passingRepo("d"), time.Unix(0, 0).UTC())
	for _, r := range rr.Results {
		if r.CheckID == "codeowners_exists" {
			t.Error("disabled check should not run")
		}
	}
}
