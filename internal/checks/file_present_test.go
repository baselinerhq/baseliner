package checks

import (
	"strings"
	"testing"

	"github.com/baselinerhq/baseliner/internal/models"
)

func filePresentPolicy(anyOf ...string) *models.Policy {
	return &models.Policy{ID: "p", Checks: []models.CheckDefinition{
		{ID: "renovate", Type: "file_present", Enabled: true, AnyOf: anyOf},
	}}
}

// file_present passes when any listed path is a file, fails when none is
// and every directory they sit in was read, and is unknown otherwise.
func TestFilePresent(t *testing.T) {
	reg, err := ForPolicy(filePresentPolicy("renovate.json", ".github/renovate.json", "config/renovate.json"))
	if err != nil {
		t.Fatal(err)
	}
	c, ok := reg.Get("renovate")
	if !ok {
		t.Fatal("file_present check not registered")
	}
	for _, tc := range []struct {
		name   string
		files  []string
		unread []string
		want   models.CheckStatus
	}{
		{"root", []string{"renovate.json"}, nil, models.StatusPass},
		{"nested", []string{"config/renovate.json"}, nil, models.StatusPass},
		{"found despite unread", []string{".github/renovate.json"}, []string{""}, models.StatusPass},
		{"same name elsewhere", []string{"docs/renovate.json", "renovate.json5"}, nil, models.StatusFail},
		{"case differs", []string{"Renovate.json"}, nil, models.StatusFail},
		{"root unread", nil, []string{""}, models.StatusUnknown},
		{"nested unread", nil, []string{"config"}, models.StatusUnknown},
		{"unrelated unread", nil, []string{"docs"}, models.StatusFail},
	} {
		repo := &models.NormalizedRepository{FS: &models.FilesystemContext{PolicyFiles: tc.files, PolicyUnreadDirs: tc.unread}}
		got := Evaluate(c, repo)
		if got.Status != tc.want {
			t.Errorf("%s: status %s, want %s", tc.name, got.Status, tc.want)
		}
		if tc.want == models.StatusFail && !strings.Contains(*got.Message, "config/renovate.json") {
			t.Errorf("%s: message %q does not list the paths", tc.name, *got.Message)
		}
	}
	if got := Evaluate(c, &models.NormalizedRepository{}); got.Status != models.StatusUnknown {
		t.Errorf("no filesystem context: status %s", got.Status)
	}
}

// A defined check may not take a built-in check's id; ExtraDirs lists the
// enabled checks' directories once each.
func TestForPolicyAndExtraDirs(t *testing.T) {
	pol := filePresentPolicy("x")
	pol.Checks[0].ID = "readme_exists"
	if _, err := ForPolicy(pol); err == nil {
		t.Error("a file_present check named readme_exists was accepted")
	}
	pol = filePresentPolicy("renovate.json", "config/a.json", "config/b.json", ".github/x.yml")
	pol.Checks = append(pol.Checks, models.CheckDefinition{ID: "off", Type: "file_present", AnyOf: []string{"skipped/x"}})
	if got := strings.Join(ExtraDirs(pol, nil), ","); got != ",.github,config" {
		t.Errorf("ExtraDirs = %q", got)
	}
}

// A check in the global ignore list runs on no repo, so its directories are
// not listed; a check defined twice is refused by name.
func TestExtraDirsIgnoreAndDuplicate(t *testing.T) {
	pol := filePresentPolicy("config/a.json")
	if got := ExtraDirs(pol, []string{"renovate"}); len(got) != 0 {
		t.Errorf("ExtraDirs of an ignored check = %v", got)
	}
	pol.Checks = append(pol.Checks, pol.Checks[0])
	if _, err := ForPolicy(pol); err == nil || !strings.Contains(err.Error(), "defined twice") {
		t.Errorf("duplicate id: err = %v", err)
	}
}
