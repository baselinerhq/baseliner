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
				Slug: "acme/good", Forge: "github", Timestamp: ts, Score: models.ScorePtr(1.0), Coverage: 1.0,
				Results: []models.CheckResult{
					{CheckID: "readme_exists", Status: models.StatusPass, Severity: models.SeverityCritical},
				},
			},
			{
				Slug: "acme/bad", Forge: "github", Timestamp: ts, Score: models.ScorePtr(0.6087), Coverage: 0.8,
				Results: []models.CheckResult{
					{CheckID: "readme_exists", Status: models.StatusFail, Severity: models.SeverityCritical, Message: sp("No README file found")},
					{CheckID: "stale_repo", Status: models.StatusUnknown, Severity: models.SeverityLow, Message: sp("Git context not available")},
				},
			},
		},
	}
}

const wantConsole = `repo                                      score  cover   pass   fail    unk
----------------------------------------------------------------------------
acme/good                                  1.00   100%      1      0      0
acme/bad                                   0.61    80%      0      1      1

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

// A scan of one forge shows no forge column; one of two shows it in the
// table, the failures list, the Markdown report and the SARIF properties, so
// the same slug on two forges can be told apart.
func TestForgeColumn(t *testing.T) {
	color.NoColor = true
	one := sampleRun()
	var buf bytes.Buffer
	PrintSummary(&buf, one)
	if strings.Contains(buf.String(), "forge") || strings.Contains(buildMarkdown(one), "Forge") {
		t.Errorf("single-forge output shows a forge column:\n%s", buf.String())
	}
	two := sampleRun()
	two.Repos[1].Forge = "gitlab"
	two.Repos[0].Slug = two.Repos[1].Slug
	buf.Reset()
	PrintSummary(&buf, two)
	out := buf.String()
	for _, want := range []string{"forge", "github", "gitlab", "acme/bad (gitlab)"} {
		if !strings.Contains(out, want) {
			t.Errorf("console lacks %q:\n%s", want, out)
		}
	}
	md := buildMarkdown(two)
	for _, want := range []string{"| Repo | Forge |", "| `acme/bad` | gitlab |", "### `acme/bad` (gitlab)"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	forges := map[string]bool{}
	for _, r := range buildSARIF(two).Runs[0].Results {
		forges[r.Properties["forge"]] = true
	}
	if !forges["gitlab"] {
		t.Errorf("SARIF forge properties = %v, want gitlab", forges)
	}
	// A masked repo has no forge: no empty parentheses and no property.
	two.Repos = append(two.Repos, two.Repos[1])
	two.Repos[2].Slug, two.Repos[2].Forge = "private/1", ""
	buf.Reset()
	PrintSummary(&buf, two)
	if md := buildMarkdown(two); strings.Contains(buf.String(), "()") || strings.Contains(md, "()") {
		t.Errorf("empty forge rendered:\n%s\n%s", buf.String(), md)
	}
	for _, r := range buildSARIF(two).Runs[0].Results {
		if f, ok := r.Properties["forge"]; ok && f == "" {
			t.Error("SARIF carries an empty forge property")
		}
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
      "forge": "github",
      "timestamp": "2026-06-17T04:00:00Z",
      "score": 1.0,
      "coverage": 1.0,
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
      "forge": "github",
      "timestamp": "2026-06-17T04:00:00Z",
      "score": 0.6087,
      "coverage": 0.8,
      "results": [
        {
          "check_id": "readme_exists",
          "status": "fail",
          "severity": "critical",
          "message": "No README file found"
        },
        {
          "check_id": "stale_repo",
          "status": "unknown",
          "severity": "low",
          "message": "Git context not available"
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
