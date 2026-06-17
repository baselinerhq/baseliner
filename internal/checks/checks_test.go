package checks

import (
	"testing"

	"github.com/baselinerhq/baseliner/internal/models"
)

func strptr(s string) *string { return &s }

// fullFS is a filesystem context that passes every hygiene check.
func fullFS() *models.FilesystemContext {
	return &models.FilesystemContext{
		KeyFiles:       map[string]bool{"README": true, "LICENSE": true, "GITIGNORE": true, "CODEOWNERS": true},
		ReadmeContent:  strptr("# Title\n\nbody"),
		CIFiles:        []string{".github/workflows/ci.yml"},
		DepUpdateFiles: []string{".github/dependabot.yml"},
	}
}

func TestHygieneChecks(t *testing.T) {
	reg := BuildDefault()
	cases := []struct {
		id     string
		mutate func(*models.FilesystemContext)
		want   models.CheckStatus
	}{
		{"readme_exists", func(fs *models.FilesystemContext) { fs.KeyFiles["README"] = false }, models.StatusFail},
		{"readme_exists", func(fs *models.FilesystemContext) {}, models.StatusPass},
		{"readme_nonempty", func(fs *models.FilesystemContext) { fs.ReadmeContent = nil }, models.StatusFail},
		{"readme_nonempty", func(fs *models.FilesystemContext) { fs.ReadmeContent = strptr("   ") }, models.StatusFail},
		{"readme_has_heading", func(fs *models.FilesystemContext) { fs.ReadmeContent = strptr("no heading here") }, models.StatusFail},
		{"readme_has_heading", func(fs *models.FilesystemContext) { fs.ReadmeContent = strptr("Title\n=====") }, models.StatusPass},
		{"readme_has_heading", func(fs *models.FilesystemContext) { fs.ReadmeContent = strptr("# Title\r\nbody") }, models.StatusPass},    // CRLF
		{"readme_has_heading", func(fs *models.FilesystemContext) { fs.ReadmeContent = strptr("intro\r# Heading\r") }, models.StatusPass}, // bare CR
		{"readme_has_heading", func(fs *models.FilesystemContext) { fs.ReadmeContent = strptr("\u00a0# Heading") }, models.StatusPass},    // non-breaking-space indent
		{"license_exists", func(fs *models.FilesystemContext) { fs.KeyFiles["LICENSE"] = false }, models.StatusFail},
		{"gitignore_exists", func(fs *models.FilesystemContext) { fs.KeyFiles["GITIGNORE"] = false }, models.StatusFail},
		{"ci_present", func(fs *models.FilesystemContext) { fs.CIFiles = nil }, models.StatusFail},
		{"codeowners_exists", func(fs *models.FilesystemContext) { fs.KeyFiles["CODEOWNERS"] = false }, models.StatusFail},
		{"dependency_update_config", func(fs *models.FilesystemContext) { fs.DepUpdateFiles = nil }, models.StatusFail},
	}
	for _, tc := range cases {
		fs := fullFS()
		tc.mutate(fs)
		repo := &models.NormalizedRepository{FS: fs}
		c, _ := reg.Get(tc.id)
		got := Evaluate(c, repo).Status
		if got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.id, got, tc.want)
		}
	}
}

func TestGitChecks(t *testing.T) {
	reg := BuildDefault()
	days := 200
	fresh := 5
	main, master := "main", "master"
	cases := []struct {
		id    string
		label string
		git   *models.GitContext
		want  models.CheckStatus
	}{
		{"default_branch_is_main", "main", &models.GitContext{DefaultBranch: &main}, models.StatusPass},
		{"default_branch_is_main", "master", &models.GitContext{DefaultBranch: &master}, models.StatusFail},
		{"default_branch_is_main", "nil", &models.GitContext{DefaultBranch: nil}, models.StatusFail},
		{"stale_repo", "fresh", &models.GitContext{IsStale: false, DaysSinceCommit: &fresh}, models.StatusPass},
		{"stale_repo", "stale", &models.GitContext{IsStale: true, DaysSinceCommit: &days}, models.StatusFail},
	}
	for _, tc := range cases {
		repo := &models.NormalizedRepository{Git: tc.git}
		c, _ := reg.Get(tc.id)
		if got := Evaluate(c, repo).Status; got != tc.want {
			t.Errorf("%s (%s): got %s, want %s", tc.id, tc.label, got, tc.want)
		}
	}
}

func TestLayerSkip(t *testing.T) {
	reg := BuildDefault()
	repo := &models.NormalizedRepository{FS: fullFS()} // no git context
	c, _ := reg.Get("default_branch_is_main")
	if got := Evaluate(c, repo).Status; got != models.StatusSkip {
		t.Errorf("git check on fs-only repo: got %s, want skip", got)
	}
}
