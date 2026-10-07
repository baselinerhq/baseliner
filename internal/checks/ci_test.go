package checks

import (
	"strings"
	"testing"

	"github.com/baselinerhq/baseliner/internal/models"
)

// ci_present must not pass a repo whose only CI is GitHub Actions workflows
// that GitHub has disabled. When workflow state is unknown (no Actions read
// access, or a local checkout), it falls back to file presence.
func TestCIPresentHonoursDisabledWorkflows(t *testing.T) {
	wf := ".github/workflows/ci.yml"
	cases := []struct {
		name     string
		files    []string
		disabled map[string]string
		want     models.CheckStatus
	}{
		{"only workflow disabled", []string{wf}, map[string]string{wf: "disabled_inactivity"}, models.StatusFail},
		{"one of two disabled", []string{wf, ".github/workflows/release.yml"}, map[string]string{wf: "disabled_manually"}, models.StatusPass},
		{"disabled workflow beside other CI", []string{wf, ".circleci/config.yml"}, map[string]string{wf: "disabled_inactivity"}, models.StatusPass},
		{"state unknown: file presence", []string{wf}, nil, models.StatusPass},
		{"state known, none disabled", []string{wf}, map[string]string{}, models.StatusPass},
		{"no CI files", nil, map[string]string{}, models.StatusFail},
	}
	c, _ := BuildDefault().Get("ci_present")
	for _, tc := range cases {
		fs := fullFS()
		fs.CIFiles = tc.files
		fs.DisabledCIFiles = tc.disabled
		if got := Evaluate(c, &models.NormalizedRepository{FS: fs}).Status; got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestCIPresentNamesDisabledWorkflows(t *testing.T) {
	fs := fullFS()
	fs.CIFiles = []string{".github/workflows/ci.yml"}
	fs.DisabledCIFiles = map[string]string{".github/workflows/ci.yml": "disabled_inactivity"}
	c, _ := BuildDefault().Get("ci_present")
	res := Evaluate(c, &models.NormalizedRepository{FS: fs})
	if res.Message == nil || !strings.Contains(*res.Message, "ci.yml (disabled_inactivity)") {
		t.Errorf("message = %v, want it to name the disabled workflow and its state", res.Message)
	}
}
