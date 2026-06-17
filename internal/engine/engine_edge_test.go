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

// panickingCheck always panics when evaluated (LayerNone so it never skips).
type panickingCheck struct{}

func (panickingCheck) ID() string          { return "boom" }
func (panickingCheck) Layer() checks.Layer { return checks.LayerNone }
func (panickingCheck) Eval(*models.NormalizedRepository) models.CheckResult {
	panic("kaboom")
}

// A panic in a check must degrade to an engine_error result, not abort the batch.
func TestEngineErrorOnPanic(t *testing.T) {
	reg := checks.NewRegistry()
	reg.Register(panickingCheck{})
	pol := &models.Policy{ID: "boom-v1", Checks: []models.CheckDefinition{
		{ID: "boom", Severity: models.SeverityCritical, Enabled: true},
	}}
	run := New(pol, reg, nil, nil).RunBatch([]*models.NormalizedRepository{passingRepo("p")}, time.Unix(0, 0).UTC())
	if run.TotalRepos != 1 || run.Failed != 1 || run.Passed != 0 {
		t.Fatalf("counts: total=%d passed=%d failed=%d, want 1/0/1", run.TotalRepos, run.Passed, run.Failed)
	}
	rr := run.Repos[0]
	if len(rr.Results) != 1 || rr.Results[0].CheckID != "engine_error" || rr.Results[0].Status != models.StatusError {
		t.Fatalf("expected single engine_error ERROR result, got %+v", rr.Results)
	}
	if rr.Score != 0 {
		t.Errorf("engine_error score = %v, want 0", rr.Score)
	}
}

// computeScore must round half-to-even (matching Python's round), not half-away.
// 17 of 32 weight units passing -> 0.53125 -> 0.5312, not 0.5313.
func TestScoreRoundsHalfToEven(t *testing.T) {
	// 17 critical-weight (4) passes vs 32 total: build 8 checks (4 pass, 4 fail)
	// is awkward; assert round4 directly on the known boundary values instead.
	cases := map[float64]float64{
		17.0 / 32.0:  0.5312,
		51.0 / 160.0: 0.3187,
		13.0 / 32.0:  0.4062,
		14.0 / 23.0:  0.6087, // the default-policy critical-fail case
	}
	for in, want := range cases {
		if got := round4(in); got != want {
			t.Errorf("round4(%v) = %v, want %v", in, got, want)
		}
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
