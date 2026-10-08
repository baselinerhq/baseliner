package runner

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Monitor mode (--fail-under 0 --min-coverage 1.0) is documented so that a red
// run means the scan broke. A repo whose every read failed must therefore fail
// --min-coverage: its file checks are unknown, not failures against files that
// look missing, which scored full coverage and let the run go green.
func TestScanUnreadableRepoFailsMinCoverage(t *testing.T) {
	healthy, err := url.Parse(fakeGitHub(t).URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(healthy)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(strings.ToLower(r.URL.Path), "/repos/acme/secret-lab/") {
			http.Error(w, `{"message":"Server Error"}`, http.StatusInternalServerError)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "test-token")

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(prev)
	cfg := filepath.Join(t.TempDir(), "baseliner.yaml")
	if err := os.WriteFile(cfg, []byte("scope:\n  github:\n    type: org\n    name: acme\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := run(Options{ConfigPath: cfg, Format: "json", FailUnder: fptr(0), MinCoverage: fptr(1.0)})
	if code != 1 {
		t.Fatalf("exit = %d, want 1: every read for acme/secret-lab failed\nstderr:\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "1 repo(s) below --min-coverage 100%: acme/secret-lab") {
		t.Errorf("only the unreadable repo should be below --min-coverage:\n%s", stderr)
	}
}

// The local collector has the same contract: a directory or README it cannot
// read is unknown, not absent, so a monitor-mode scan of such a checkout goes
// red on --min-coverage instead of failing checks against files that exist.
func TestScanUnreadableLocalRepoFailsMinCoverage(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permissions do not restrict root")
	}
	root := t.TempDir()
	for _, f := range []string{"README.md", "LICENSE", ".gitignore", ".github/CODEOWNERS", ".github/dependabot.yml", ".github/workflows/ci.yml"} {
		p := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("# x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{filepath.Join(root, ".github"), filepath.Join(root, "README.md")} {
		if err := os.Chmod(p, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(p, 0o755) })
	}
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(prev)
	cfg := filepath.Join(t.TempDir(), "baseliner.yaml")
	body := "scope:\n  local:\n    paths: [\"" + root + "\"]\npolicy:\n  ignore: [default_branch_is_main, stale_repo]\n"
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := run(Options{ConfigPath: cfg, Format: "json", FailUnder: fptr(0), MinCoverage: fptr(1.0)})
	if code != 1 || !strings.Contains(stderr, "below --min-coverage") {
		t.Errorf("exit = %d, want 1 on --min-coverage\nstderr:\n%s", code, stderr)
	}
}
