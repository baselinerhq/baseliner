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
