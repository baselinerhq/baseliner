package runner

import (
	"testing"

	"github.com/baselinerhq/baseliner/internal/checks"
	"github.com/baselinerhq/baseliner/internal/models"
)

func TestNeedsPlatform(t *testing.T) {
	reg := checks.BuildDefault()
	def := func(id string, enabled bool) models.CheckDefinition {
		return models.CheckDefinition{ID: id, Enabled: enabled}
	}
	cases := []struct {
		name   string
		checks []models.CheckDefinition
		ignore []string
		want   bool
	}{
		{"fs and git checks only", []models.CheckDefinition{def("readme_exists", true), def("stale_repo", true)}, nil, false},
		{"platform check enabled", []models.CheckDefinition{def("readme_exists", true), def("no_exempt_bypass", true)}, nil, true},
		{"platform check disabled", []models.CheckDefinition{def("default_branch_requires_review", false)}, nil, false},
		{"platform check ignored globally", []models.CheckDefinition{def("no_exempt_bypass", true)}, []string{"no_exempt_bypass"}, false},
	}
	for _, tc := range cases {
		if got := needsPlatform(&models.Policy{Checks: tc.checks}, reg, tc.ignore); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
