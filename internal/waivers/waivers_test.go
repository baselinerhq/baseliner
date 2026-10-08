package waivers

import (
	"strings"
	"testing"
	"time"

	"github.com/baselinerhq/baseliner/internal/models"
)

func TestParse(t *testing.T) {
	ws, err := Parse([]byte(`waivers:
  - check: ci_present
    reason: docs-only repo, nothing to build
  - check: license_exists
    reason: internal tooling
    until: 2027-01-01
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(ws) != 2 || ws[0].Check != "ci_present" || ws[0].Until != nil || ws[1].Until == nil || ws[1].Until.Format("2006-01-02") != "2027-01-01" {
		t.Errorf("waivers = %+v", ws)
	}
}

// A waiver must say why, and the file is strict, so a typo is an error rather
// than a waiver that silently does something else.
func TestParseRejects(t *testing.T) {
	for name, body := range map[string]string{
		"no reason":   "waivers:\n  - check: ci_present\n",
		"no check":    "waivers:\n  - reason: x\n",
		"unknown key": "waivers:\n  - check: ci_present\n    reason: x\n    untill: 2027-01-01\n",
		"bad date":    "waivers:\n  - check: ci_present\n    reason: x\n    until: next year\n",
		"top-level":   "waiver:\n  - check: ci_present\n    reason: x\n",
		"repeated":    "waivers:\n  - check: ci_present\n    reason: x\n  - check: ci_present\n    reason: y\n",
		"not yaml":    "waivers: [",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(body)); err == nil {
				t.Error("want an error")
			}
		})
	}
}

// A waiver applies through its until date, in UTC, and not after.
func TestActive(t *testing.T) {
	until := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	w := models.Waiver{Check: "ci_present", Reason: "x", Until: &until}
	for _, c := range []struct {
		now  time.Time
		want bool
	}{
		{time.Date(2026, 12, 31, 23, 0, 0, 0, time.UTC), true},
		{time.Date(2027, 1, 1, 23, 59, 0, 0, time.UTC), true},
		{time.Date(2027, 1, 2, 0, 0, 0, 0, time.UTC), false},
	} {
		if got := w.Active(c.now); got != c.want {
			t.Errorf("Active(%s) = %v, want %v", c.now, got, c.want)
		}
	}
	if !(models.Waiver{Check: "x", Reason: "y"}).Active(time.Now()) {
		t.Error("a waiver without until never expires")
	}
	if !strings.Contains(Path, "baseliner") {
		t.Errorf("Path = %q", Path)
	}
}
