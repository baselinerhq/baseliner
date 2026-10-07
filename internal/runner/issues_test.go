package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// With --open-issues, delivering findings is part of the job. In monitor mode
// (--fail-under 0) a run whose findings issues could not be written must not
// exit 0: that would be a green run that delivered nothing. fakeGitHub 404s
// every issue write, and its private repo has findings.
func TestScanFailsWhenFindingsIssuesCannotBeWritten(t *testing.T) {
	srv := fakeGitHub(t)
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "test-token")

	cfg := filepath.Join(t.TempDir(), "baseliner.yaml")
	if err := os.WriteFile(cfg, []byte("scope:\n  github:\n    type: org\n    name: acme\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := run(Options{ConfigPath: cfg, Format: "table", OpenIssues: true, FailUnder: fptr(0)})
	if code != 2 {
		t.Fatalf("exit = %d, want 2 when a findings issue could not be written\nstderr:\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "findings issue") {
		t.Errorf("stderr does not explain the failure:\n%s", stderr)
	}
}
