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
	if posture, ok := rr.Posture(); !ok || posture != 1.0 {
		t.Errorf("posture = %v (defined=%v), want 1.0 (unknown check ignored)", posture, ok)
	}
}

// A repo whose every check is unobservable must NOT report a posture. Before
// this behaviour was fixed it scored 1.0, i.e. absence of evidence read as
// perfect compliance — the regression this test exists to prevent.
func TestAllUnobservedHasNoPostureAndZeroCoverage(t *testing.T) {
	// Policy of only git checks against a repo with no git context: both
	// unobservable.
	pol := &models.Policy{ID: "git-only", Checks: []models.CheckDefinition{
		{ID: "default_branch_is_main", Severity: models.SeverityMedium, Enabled: true},
		{ID: "stale_repo", Severity: models.SeverityLow, Enabled: true},
	}}
	repo := passingRepo("s")
	repo.Git = nil
	rr := New(pol, checks.BuildDefault(), nil, nil).Run(repo, time.Unix(0, 0).UTC())

	if _, ok := rr.Posture(); ok {
		t.Errorf("posture = %v, want undefined (nil) when nothing was observed", *rr.Score)
	}
	if rr.Coverage != 0 {
		t.Errorf("coverage = %v, want 0", rr.Coverage)
	}
	for _, r := range rr.Results {
		if r.Status != models.StatusUnknown {
			t.Errorf("expected unknown, got %s for %s", r.Status, r.CheckID)
		}
	}
}

// Coverage must report the observed fraction by severity weight, and unobserved
// checks must not raise posture.
func TestPartialCoverage(t *testing.T) {
	// Eight fs checks (observable, all pass) + two git checks (unobservable).
	// Default severities: the two git checks are medium(2) and low(1) = 3 of 23.
	pol := defaultPolicy()
	repo := passingRepo("p")
	repo.Git = nil
	rr := New(pol, checks.BuildDefault(), nil, nil).Run(repo, time.Unix(0, 0).UTC())

	posture, ok := rr.Posture()
	if !ok {
		t.Fatal("posture undefined, want defined from the observable checks")
	}
	if posture != 1.0 {
		t.Errorf("posture = %v, want 1.0 (every observed check passed)", posture)
	}
	want := models.Score(round4(20.0 / 23.0))
	if rr.Coverage != want {
		t.Errorf("coverage = %v, want %v (20 of 23 weight observed)", rr.Coverage, want)
	}
}

// A repo with no conclusive result must fail the default gate. Previously it
// scored 1.0 and counted as passed, so a wholly unobservable repo turned a fleet
// scan green.
func TestAllUnobservedFailsDefaultGate(t *testing.T) {
	pol := &models.Policy{ID: "git-only", Checks: []models.CheckDefinition{
		{ID: "default_branch_is_main", Severity: models.SeverityMedium, Enabled: true},
		{ID: "stale_repo", Severity: models.SeverityLow, Enabled: true},
	}}
	repo := passingRepo("s")
	repo.Git = nil
	run := New(pol, checks.BuildDefault(), nil, nil).RunBatch(
		[]*models.NormalizedRepository{repo}, time.Unix(0, 0).UTC())
	if run.Passed != 0 || run.Failed != 1 {
		t.Errorf("unobservable repo: passed=%d failed=%d, want 0/1", run.Passed, run.Failed)
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
	repo := passingRepo("p")
	repo.SourceType = models.SourceGitHub
	run := New(pol, reg, nil, nil).RunBatch([]*models.NormalizedRepository{repo}, time.Unix(0, 0).UTC())
	if run.TotalRepos != 1 || run.Failed != 1 || run.Passed != 0 {
		t.Fatalf("counts: total=%d passed=%d failed=%d, want 1/0/1", run.TotalRepos, run.Passed, run.Failed)
	}
	rr := run.Repos[0]
	if len(rr.Results) != 1 || rr.Results[0].CheckID != "engine_error" || rr.Results[0].Status != models.StatusError {
		t.Fatalf("expected single engine_error ERROR result, got %+v", rr.Results)
	}
	// --open-issues finds the repo's source by forge and slug.
	if rr.Forge != "github" {
		t.Errorf("engine_error Forge = %q, want github", rr.Forge)
	}
	// A panic means the repo was not assessed, so it reports no posture rather
	// than a score of 0 (which would imply it was assessed and wholly failed).
	if _, ok := rr.Posture(); ok {
		t.Errorf("engine_error posture = %v, want undefined", *rr.Score)
	}
	if rr.Coverage != 0 {
		t.Errorf("engine_error coverage = %v, want 0", rr.Coverage)
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
