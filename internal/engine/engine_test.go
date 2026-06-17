package engine

import (
	"testing"
	"time"

	"github.com/baselinerhq/baseliner/internal/checks"
	"github.com/baselinerhq/baseliner/internal/models"
)

func strptr(s string) *string { return &s }

// passingRepo passes every check (fs + git, default branch main, fresh).
func passingRepo(slug string) *models.NormalizedRepository {
	fresh := 1
	return &models.NormalizedRepository{
		Slug: slug,
		FS: &models.FilesystemContext{
			KeyFiles:       map[string]bool{"README": true, "LICENSE": true, "GITIGNORE": true, "CODEOWNERS": true},
			ReadmeContent:  strptr("# Title\n"),
			CIFiles:        []string{"ci.yml"},
			DepUpdateFiles: []string{"dependabot.yml"},
		},
		Git: &models.GitContext{DefaultBranch: "main", IsStale: false, DaysSinceCommit: &fresh},
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
	if rr.Score != 1.0 {
		t.Errorf("score = %v, want 1.0", rr.Score)
	}
}

func TestCriticalFailScore(t *testing.T) {
	// Drop the README: fails readme_exists(4) + readme_nonempty(3) + readme_has_heading(2).
	repo := passingRepo("no-readme")
	repo.FS.KeyFiles["README"] = false
	repo.FS.ReadmeContent = nil
	rr := newEngine().Run(repo, time.Unix(0, 0).UTC())
	// total weight = 4+3+2+3+2+3+1+2+2+1 = 23; passed = 23-9 = 14; 14/23 = 0.6087
	if rr.Score != 0.6087 {
		t.Errorf("score = %v, want 0.6087", rr.Score)
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
