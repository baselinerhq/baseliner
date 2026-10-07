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
// and 404s everything else, so every issue search and write fails. extra
// adds handlers, e.g. a working search so that only the write fails.
func fakeOrg(t *testing.T, repos string, extra ...func(*http.ServeMux)) {
	t.Helper()
	mux := http.NewServeMux()
	for _, add := range extra {
		add(mux)
	}
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
// exit 0: that would be a green run that delivered nothing. Here the search
// works and only the write is denied, as for a token with Issues read but not
// write.
func TestScanFailsWhenFindingsIssuesCannotBeWritten(t *testing.T) {
	fakeOrg(t, `[{"name":"a","owner":{"login":"acme"},"visibility":"public","default_branch":"main"}]`,
		func(mux *http.ServeMux) {
			mux.HandleFunc("GET /repos/acme/a/issues", func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`[]`))
			})
			mux.HandleFunc("GET /repos/acme/a/labels/baseliner", func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"name":"baseliner"}`))
			})
			mux.HandleFunc("POST /repos/acme/a/issues", func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `{"message":"Resource not accessible by personal access token"}`, http.StatusForbidden)
			})
		})

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
	if code != 0 || strings.Contains(stderr, "could not deliver") {
		t.Errorf("exit = %d, want 0 with the issues-disabled repo skipped\nstderr:\n%s", code, stderr)
	}
}

// Archived repos are read-only, so they cannot take a findings issue either.
func TestScanSkipsArchivedRepos(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "baseliner.yaml")
	body := "scope:\n  github:\n    type: org\n    name: acme\n    include_archived: true\n"
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeOrg(t, `[{"name":"a","owner":{"login":"acme"},"visibility":"public","default_branch":"main","archived":true}]`)
	code, _, stderr := run(Options{ConfigPath: cfg, Format: "table", OpenIssues: true, FailUnder: fptr(0)})
	if code != 0 || strings.Contains(stderr, "could not deliver") {
		t.Errorf("exit = %d, want 0 with the archived repo skipped\nstderr:\n%s", code, stderr)
	}
}

// --min-coverage prints the repos below it. A local checkout has no platform
// context, so a platform-only policy leaves coverage at 0.
func TestScanMinCoveragePrintsRepos(t *testing.T) {
	dir := t.TempDir()
	pol := filepath.Join(dir, "policy.yaml")
	if err := os.WriteFile(pol, []byte("id: p\nchecks:\n  - { id: no_exempt_bypass, severity: high }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "baseliner.yaml")
	body := "scope:\n  local:\n    paths:\n      - " + dir + "\npolicy:\n  base: " + pol + "\n"
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := run(Options{ConfigPath: cfg, Format: "table", FailUnder: fptr(0), MinCoverage: fptr(1)})
	if code != 1 || !strings.Contains(stderr, "below --min-coverage 100%") {
		t.Errorf("exit = %d, want 1 with the repo listed below --min-coverage\nstderr:\n%s", code, stderr)
	}
}
