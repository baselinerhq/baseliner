package waivers

import (
	"strings"
	"testing"
	"time"

	"github.com/baselinerhq/baseliner/internal/models"
)

func TestParse(t *testing.T) {
	ws, err := Parse([]byte(`version: 1
waivers:
  - check: ci_present
    reason: "  docs-only repo, nothing to build  "
  - check: license_exists
    reason: internal tooling
    until: 2027-01-01
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(ws) != 2 || ws[0].Check != "ci_present" || ws[0].Reason != "docs-only repo, nothing to build" || ws[0].Until != nil ||
		ws[1].Until == nil || ws[1].Until.Format("2006-01-02") != "2027-01-01" {
		t.Errorf("waivers = %+v", ws)
	}
}

// An empty or waiver-less file declares nothing, without an error.
func TestParseEmpty(t *testing.T) {
	for _, body := range []string{"", "# nothing yet\n", "waivers: []\n", "version: 1\n"} {
		if ws, err := Parse([]byte(body)); err != nil || ws != nil {
			t.Errorf("Parse(%q) = %v, %v; want no waivers and no error", body, ws, err)
		}
	}
}

// The file is strict, so a typo is an error rather than a waiver that
// silently does something else. The errors never repeat the file's values,
// which may come from a private repo.
func TestParseRejects(t *testing.T) {
	const secret = "ZZQSECRET"
	for name, body := range map[string]string{
		"no reason":         "waivers:\n  - check: " + secret + "\n",
		"blank reason":      "waivers:\n  - check: ci_present\n    reason: '   '\n",
		"no check":          "waivers:\n  - reason: " + secret + "\n",
		"unknown key":       "waivers:\n  - check: ci_present\n    reason: x\n    " + secret + ": 2027-01-01\n",
		"bad date":          "waivers:\n  - check: ci_present\n    reason: x\n    until: " + secret + "\n",
		"wrong type":        "waivers: " + secret + "\n",
		"top-level":         "waiver:\n  - check: ci_present\n    reason: " + secret + "\n",
		"repeated":          "waivers:\n  - check: " + secret + "\n    reason: x\n  - check: " + secret + "\n    reason: y\n",
		"not yaml":          "waivers: [" + secret,
		"second document":   "waivers:\n  - check: ci_present\n    reason: x\n---\nwaivers:\n  - check: " + secret + "\n    reason: y\n",
		"newer version":     "version: 2\nwaivers:\n  - check: ci_present\n    reason: " + secret + "\n",
		"long reason":       "waivers:\n  - check: ci_present\n    reason: " + strings.Repeat("x", MaxReasonLen+1) + secret + "\n",
		"over the size cap": "waivers:\n  - check: ci_present\n    reason: " + secret + "\n#" + strings.Repeat("x", MaxBytes) + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(body))
			if err == nil {
				t.Fatal("want an error")
			}
			if strings.Contains(err.Error(), secret) {
				t.Errorf("the error repeats a value from the file: %v", err)
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
}
