package runner

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func orgConfig(t *testing.T) string {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "baseliner.yaml")
	if err := os.WriteFile(cfg, []byte("scope:\n  github:\n    type: org\n    name: acme\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// fakeOrg serves an org whose repos have no files, so every repo has findings,
// and 404s everything else, so every issue search and write fails.
func fakeOrg(t *testing.T, repos string) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rate_limit", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"resources":{"core":{"limit":5000,"remaining":5000,"reset":0}}}`))
	})
	mux.HandleFunc("GET /orgs/acme/repos", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(repos))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "test-token")
}

// With --open-issues, delivering findings is part of the job. In monitor mode
// (--fail-under 0) a run whose findings issues could not be written must not
// exit 0: that would be a green run that delivered nothing. fakeGitHub 404s
// every issue write, and its private repo has findings.
func TestScanFailsWhenFindingsIssuesCannotBeWritten(t *testing.T) {
	srv := fakeGitHub(t)
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "test-token")

	code, _, stderr := run(Options{ConfigPath: orgConfig(t), Format: "table", OpenIssues: true, FailUnder: fptr(0)})
	if code != 2 {
		t.Fatalf("exit = %d, want 2 when a findings issue could not be written\nstderr:\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "findings issue") {
		t.Errorf("stderr does not explain the failure:\n%s", stderr)
	}
}

// One failed repo must not stop delivery to the rest, and the summary counts
// every repo that failed.
func TestScanCountsEveryFailedIssueWrite(t *testing.T) {
	fakeOrg(t, `[{"name":"a","owner":{"login":"acme"},"visibility":"public","default_branch":"main"},
		{"name":"b","owner":{"login":"acme"},"visibility":"public","default_branch":"main"}]`)
	code, _, stderr := run(Options{ConfigPath: orgConfig(t), Format: "table", OpenIssues: true, FailUnder: fptr(0)})
	if code != 2 || !strings.Contains(stderr, "for 2 repo(s)") {
		t.Errorf("exit = %d, want 2 with both repos counted\nstderr:\n%s", code, stderr)
	}
}

// The gates still run and print when an issue write fails; the exit is the
// worse of the two.
func TestScanIssueFailureKeepsGateOutput(t *testing.T) {
	srv := fakeGitHub(t)
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "test-token")

	code, _, stderr := run(Options{ConfigPath: orgConfig(t), Format: "table", OpenIssues: true, FailUnder: fptr(0.99)})
	if code != 2 {
		t.Errorf("exit = %d, want 2 (an issue failure outranks a gate failure)", code)
	}
	for _, want := range []string{"below --fail-under", "findings issue"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q:\n%s", want, stderr)
		}
	}
}

// A repo with Issues disabled cannot receive a findings issue by design. That
// is not a delivery failure, or such a repo would turn every run red.
func TestScanSkipsReposWithIssuesDisabled(t *testing.T) {
	fakeOrg(t, `[{"name":"a","owner":{"login":"acme"},"visibility":"public","default_branch":"main","has_issues":false}]`)
	code, _, stderr := run(Options{ConfigPath: orgConfig(t), Format: "table", OpenIssues: true, FailUnder: fptr(0)})
	if code != 0 || strings.Contains(stderr, "could not write") {
		t.Errorf("exit = %d, want 0 with the issues-disabled repo skipped\nstderr:\n%s", code, stderr)
	}
}
