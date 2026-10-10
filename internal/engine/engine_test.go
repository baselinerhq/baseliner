package engine

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/baselinerhq/baseliner/internal/checks"
	"github.com/baselinerhq/baseliner/internal/models"
)

func strptr(s string) *string { return &s }

// passingRepo passes every check (fs + git, default branch main, fresh).
func passingRepo(slug string) *models.NormalizedRepository {
	fresh := 1
	pushed := time.Now().AddDate(0, 0, -fresh)
	main := "main"
	return &models.NormalizedRepository{
		Slug: slug,
		FS: &models.FilesystemContext{
			KeyFiles:       map[string]bool{"README": true, "LICENSE": true, "GITIGNORE": true, "CODEOWNERS": true},
			ReadmeContent:  strptr("# Title\n"),
			CIFiles:        []string{"ci.yml"},
			DepUpdateFiles: []string{"dependabot.yml"},
		},
		Git: &models.GitContext{DefaultBranch: &main, IsStale: false, LastCommitAt: &pushed, DaysSinceCommit: &fresh},
	}
}

func defaultPolicy() *models.Policy {
	sev := map[string]models.Severity{
		"readme_exists": models.SeverityCritical, "readme_nonempty": models.SeverityHigh,
		"readme_has_heading": models.SeverityMedium, "license_exists": models.SeverityHigh,
		"gitignore_exists": models.SeverityMedium, "ci_present": models.SeverityHigh,
		"codeowners_exists": models.SeverityLow, "dependency_update_config": models.SeverityMedium,
		"default_branch_is_main": models.SeverityMedium, "stale_repo": models.SeverityLow,
	}
	order := []string{"readme_exists", "readme_nonempty", "readme_has_heading", "license_exists",
		"gitignore_exists", "ci_present", "codeowners_exists", "dependency_update_config",
		"default_branch_is_main", "stale_repo"}
	p := &models.Policy{ID: "default-v1"}
	for _, id := range order {
		p.Checks = append(p.Checks, models.CheckDefinition{ID: id, Severity: sev[id], Enabled: true})
	}
	return p
}

func newEngine() *Engine {
	return New(defaultPolicy(), checks.BuildDefault(), nil, nil)
}

func TestPerfectScore(t *testing.T) {
	rr := newEngine().Run(passingRepo("ok"), time.Unix(0, 0).UTC())
	if posture, ok := rr.Posture(); !ok || posture != 1.0 {
		t.Errorf("posture = %v (defined=%v), want 1.0", posture, ok)
	}
}

func TestCriticalFailScore(t *testing.T) {
	// Drop the README: fails readme_exists(4) + readme_nonempty(3) + readme_has_heading(2).
	repo := passingRepo("no-readme")
	repo.FS.KeyFiles["README"] = false
	repo.FS.ReadmeContent = nil
	rr := newEngine().Run(repo, time.Unix(0, 0).UTC())
	// total weight = 4+3+2+3+2+3+1+2+2+1 = 23; passed = 23-9 = 14; 14/23 = 0.6087
	if posture, ok := rr.Posture(); !ok || posture != 0.6087 {
		t.Errorf("posture = %v (defined=%v), want 0.6087", posture, ok)
	}
}

func TestPolicyMetadataPropagates(t *testing.T) {
	p := &models.Policy{ID: "p", Checks: []models.CheckDefinition{
		{ID: "readme_exists", Severity: models.SeverityCritical, Enabled: true,
			PolicyInfo: "READMEs are required.", PolicyURL: "https://example.com/std"},
	}}
	rr := New(p, checks.BuildDefault(), nil, nil).Run(passingRepo("ok"), time.Unix(0, 0).UTC())
	if len(rr.Results) != 1 {
		t.Fatalf("got %d results, want 1", len(rr.Results))
	}
	got := rr.Results[0]
	if got.PolicyInfo != "READMEs are required." || got.PolicyURL != "https://example.com/std" {
		t.Errorf("policy metadata not propagated: info=%q url=%q", got.PolicyInfo, got.PolicyURL)
	}
}

func TestGlobalIgnore(t *testing.T) {
	e := New(defaultPolicy(), checks.BuildDefault(), []string{"codeowners_exists"}, nil)
	rr := e.Run(passingRepo("ig"), time.Unix(0, 0).UTC())
	for _, r := range rr.Results {
		if r.CheckID == "codeowners_exists" {
			t.Error("ignored check should not appear in results")
		}
	}
	if len(rr.Results) != 9 {
		t.Errorf("got %d results, want 9 after ignore", len(rr.Results))
	}
}

func TestBatchCounts(t *testing.T) {
	fail := passingRepo("bad")
	fail.FS.KeyFiles["LICENSE"] = false
	run := newEngine().RunBatch([]*models.NormalizedRepository{passingRepo("good"), fail}, time.Unix(0, 0).UTC())
	if run.TotalRepos != 2 || run.Passed != 1 || run.Failed != 1 {
		t.Errorf("counts = total %d passed %d failed %d, want 2/1/1", run.TotalRepos, run.Passed, run.Failed)
	}
	if len(run.RunID) != 36 {
		t.Errorf("run id %q not a uuid", run.RunID)
	}
}

// A visibility rule waives its checks on repos with a listed visibility, and
// only there. A repo with no visibility, such as a local checkout, matches
// none.
func TestIgnoreWhenVisibility(t *testing.T) {
	e := newEngine()
	e.IgnoreWhen = []VisibilityIgnore{{Visibility: []string{"private", "internal"}, Checks: []string{"license_exists", "codeowners_exists"}}}
	for _, c := range []struct {
		visibility string
		waived     bool
	}{{"private", true}, {"internal", true}, {"public", false}, {"", false}} {
		repo := passingRepo("v")
		repo.Visibility = c.visibility
		rr := e.Run(repo, time.Unix(0, 0).UTC())
		has := map[string]bool{}
		for _, r := range rr.Results {
			has[r.CheckID] = true
		}
		if has["license_exists"] == c.waived || has["codeowners_exists"] == c.waived || !has["readme_exists"] {
			t.Errorf("visibility %q: results %v, want license_exists and codeowners_exists waived = %v", c.visibility, has, c.waived)
		}
	}
}

// A repo_ignores key of the bare slug applies on every forge; one prefixed
// "<forge>:" applies only to that forge's repo of the slug.
func TestRepoIgnoresForgeKey(t *testing.T) {
	e := New(defaultPolicy(), checks.BuildDefault(), nil, map[string][]string{
		"acme/kit":        {"stale_repo"},
		"gitlab:acme/kit": {"license_exists"},
	})
	for _, c := range []struct {
		source      models.SourceType
		licenseKept bool
	}{{models.SourceGitHub, true}, {models.SourceGitLab, false}} {
		repo := passingRepo("acme/kit")
		repo.SourceType = c.source
		rr := e.Run(repo, time.Unix(0, 0).UTC())
		has := map[string]bool{}
		for _, r := range rr.Results {
			has[r.CheckID] = true
		}
		if has["stale_repo"] || has["license_exists"] != c.licenseKept {
			t.Errorf("%s: results %v, want stale_repo ignored and license_exists kept = %v", c.source, has, c.licenseKept)
		}
		if rr.Forge != string(c.source) {
			t.Errorf("%s: Forge = %q", c.source, rr.Forge)
		}
	}
}

// Rules combine: a check is waived if any rule that matches the repo lists it,
// on top of ignore and repo_ignores.
func TestIgnoreWhenRulesCombine(t *testing.T) {
	e := New(defaultPolicy(), checks.BuildDefault(), []string{"stale_repo"}, nil)
	e.IgnoreWhen = []VisibilityIgnore{
		{Visibility: []string{"public"}, Checks: []string{"gitignore_exists"}},
		{Visibility: []string{"private"}, Checks: []string{"license_exists"}},
		{Visibility: []string{"private", "internal"}, Checks: []string{"codeowners_exists"}},
	}
	repo := passingRepo("v")
	repo.Visibility = "private"
	has := map[string]bool{}
	for _, r := range e.Run(repo, time.Unix(0, 0).UTC()).Results {
		has[r.CheckID] = true
	}
	if has["license_exists"] || has["codeowners_exists"] || has["stale_repo"] || !has["gitignore_exists"] {
		t.Errorf("results %v: want license_exists, codeowners_exists and stale_repo waived, gitignore_exists kept", has)
	}
}

// A repo's waiver replaces a check's result with "waived" and its reason,
// out of both score and coverage, but only when the policy lets repos waive
// that check and the waiver has not expired.
func TestRepoWaivers(t *testing.T) {
	past := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	repo := passingRepo("w")
	repo.FS.KeyFiles["LICENSE"] = false // license_exists would fail
	repo.FS.CIFiles = nil               // ci_present would fail
	repo.Waivers = []models.Waiver{
		{Check: "license_exists", Reason: "internal tooling"},
		{Check: "ci_present", Reason: "docs only", Until: &past},
		{Check: "codeowners_exists", Reason: "solo repo"},
	}
	status := func(e *Engine) (map[string]models.CheckResult, models.RepoResult) {
		rr := e.Run(repo, now)
		m := map[string]models.CheckResult{}
		for _, r := range rr.Results {
			m[r.CheckID] = r
		}
		return m, rr
	}

	off := newEngine()
	got, _ := status(off)
	if got["license_exists"].Status != models.StatusFail {
		t.Errorf("without policy.repo_waivers, license_exists = %s, want fail", got["license_exists"].Status)
	}

	on := newEngine()
	on.WaivableChecks = map[string]bool{"license_exists": true, "ci_present": true}
	got, rr := status(on)
	lic := got["license_exists"]
	if lic.Status != models.StatusWaived || lic.Message == nil || !strings.Contains(*lic.Message, "internal tooling") || lic.Severity != models.SeverityHigh {
		t.Errorf("license_exists = %+v, want waived with the reason and the policy severity", lic)
	}
	if got["ci_present"].Status != models.StatusFail {
		t.Errorf("an expired waiver applied: ci_present = %s", got["ci_present"].Status)
	}
	if got["codeowners_exists"].Status != models.StatusPass {
		t.Errorf("a waiver the policy does not allow applied: codeowners_exists = %s", got["codeowners_exists"].Status)
	}
	// Only ci_present (high, weight 3) fails now; the waived license_exists
	// counts in neither ratio. Weights: critical 4, high 3, medium 2, low 1.
	// Without license_exists the conclusive weight is 4+3+2+2+3+1+2+2+1 = 20
	// and 3 of it fails: 17/20. Counted as a pass instead, it would be 20/23.
	if rr.Coverage != 1 || rr.Score == nil || *rr.Score != models.Score(0.85) {
		t.Errorf("score %v coverage %v: want 0.85 and 1, with the waived check out of both", *rr.Score, rr.Coverage)
	}
}

// The waived message carries the reason and, when set, the last day.
func TestRepoWaiverMessage(t *testing.T) {
	until := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	repo := passingRepo("w")
	repo.Waivers = []models.Waiver{{Check: "ci_present", Reason: "docs only", Until: &until}}
	e := newEngine()
	e.WaivableChecks = map[string]bool{"ci_present": true}
	for _, r := range e.Run(repo, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)).Results {
		if r.CheckID == "ci_present" && (r.Message == nil || *r.Message != "waived by the repo: docs only (until 2027-01-01)") {
			t.Errorf("message = %v", r.Message)
		}
	}
}

// A waiver naming something that is not a check is logged without its text,
// which comes from the repo's file.
func TestRepoWaiverUnknownCheckNotLogged(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)
	repo := passingRepo("w")
	repo.Waivers = []models.Waiver{{Check: "ZZQSECRET", Reason: "x"}}
	e := newEngine()
	e.WaivableChecks = map[string]bool{"ci_present": true}
	e.Run(repo, time.Now())
	if strings.Contains(buf.String(), "ZZQSECRET") || !strings.Contains(buf.String(), "(not a check)") {
		t.Errorf("log = %s", buf.String())
	}
}
