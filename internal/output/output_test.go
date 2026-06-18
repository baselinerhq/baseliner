package output

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/fatih/color"

	"github.com/baselinerhq/baseliner/internal/models"
)

func sp(s string) *string { return &s }

func sampleRun() *models.RunResult {
	ts := time.Date(2026, 6, 17, 4, 0, 0, 0, time.UTC)
	return &models.RunResult{
		RunID:      "11111111-2222-4333-8444-555555555555",
		Timestamp:  ts,
		TotalRepos: 2,
		Passed:     1,
		Failed:     1,
		Repos: []models.RepoResult{
			{
				Slug: "acme/good", Timestamp: ts, Score: 1.0,
				Results: []models.CheckResult{
					{CheckID: "readme_exists", Status: models.StatusPass, Severity: models.SeverityCritical},
				},
			},
			{
				Slug: "acme/bad", Timestamp: ts, Score: 0.6087,
				Results: []models.CheckResult{
					{CheckID: "readme_exists", Status: models.StatusFail, Severity: models.SeverityCritical, Message: sp("No README file found")},
					{CheckID: "stale_repo", Status: models.StatusSkip, Severity: models.SeverityLow},
				},
			},
		},
	}
}

const wantConsole = `repo                                      score   pass   fail   skip
--------------------------------------------------------------------
acme/good                                  1.00      1      0      0
acme/bad                                   0.61      0      1      1

Critical/high failures:
  acme/bad
    [CRITICAL] readme_exists: No README file found

2 repos scanned — 1 passed, 1 failed
`

func TestConsoleGolden(t *testing.T) {
	color.NoColor = true // deterministic, no ANSI
	var buf bytes.Buffer
	PrintSummary(&buf, sampleRun())
	if buf.String() != wantConsole {
		t.Errorf("console mismatch:\n--- got ---\n%s\n--- want ---\n%s", buf.String(), wantConsole)
	}
}

func TestConsolePrivacyNote(t *testing.T) {
	color.NoColor = true
	cases := map[string]string{
		"redact":  "2 private repo(s) redacted from public output.",
		"exclude": "2 private repo(s) hidden from public output.",
	}
	for mode, want := range cases {
		r := sampleRun()
		r.Privacy = &models.PrivacyNote{Mode: mode, Count: 2}
		var buf bytes.Buffer
		PrintSummary(&buf, r)
		if !strings.Contains(buf.String(), want) {
			t.Errorf("mode %q: missing %q in:\n%s", mode, want, buf.String())
		}
	}
}

const wantJSON = `{
  "run_id": "11111111-2222-4333-8444-555555555555",
  "timestamp": "2026-06-17T04:00:00Z",
  "total_repos": 2,
  "passed": 1,
  "failed": 1,
  "repos": [
    {
      "slug": "acme/good",
      "timestamp": "2026-06-17T04:00:00Z",
      "score": 1.0,
      "results": [
        {
          "check_id": "readme_exists",
          "status": "pass",
          "severity": "critical",
          "message": null
        }
      ]
    },
    {
      "slug": "acme/bad",
      "timestamp": "2026-06-17T04:00:00Z",
      "score": 0.6087,
      "results": [
        {
          "check_id": "readme_exists",
          "status": "fail",
          "severity": "critical",
          "message": "No README file found"
        },
        {
          "check_id": "stale_repo",
          "status": "skip",
          "severity": "low",
          "message": null
        }
      ]
    }
  ]
}`

func TestJSONGolden(t *testing.T) {
	got, err := marshalJSON(sampleRun())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != wantJSON {
		t.Errorf("json mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, wantJSON)
	}
}
