package actions

import (
	"testing"
	"time"

	"github.com/baselinerhq/baseliner/internal/models"
)

func sp(s string) *string { return &s }

func TestBuildBodyGolden(t *testing.T) {
	now := time.Date(2026, 6, 17, 4, 5, 0, 0, time.UTC)
	result := models.RepoResult{
		Slug:  "acme/svc",
		Score: 0.6087,
		Results: []models.CheckResult{
			{CheckID: "readme_exists", Status: models.StatusFail, Severity: models.SeverityCritical, Message: sp("No README file found")},
			{CheckID: "license_exists", Status: models.StatusPass, Severity: models.SeverityHigh},
			{CheckID: "stale_repo", Status: models.StatusSkip, Severity: models.SeverityLow},
		},
	}
	want := "## baseliner findings\n\n" +
		"**Score**: 61%  \n" +
		"**Scanned**: 2026-06-17 04:05 UTC\n\n" +
		"| check | status | severity | message |\n" +
		"|---|---|---|---|\n" +
		"| `readme_exists` | ❌ fail | critical | No README file found |\n" +
		"| `license_exists` | ✅ pass | high |  |\n" +
		"| `stale_repo` | ⏭️ skip | low |  |\n\n" +
		"---\n" +
		"*managed by [baseliner](https://github.com/baselinerhq/baseliner)*"

	if got := BuildBody(result, now); got != want {
		t.Errorf("body mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
