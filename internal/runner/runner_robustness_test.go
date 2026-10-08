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
	"strconv"
	"strings"
	"testing"
	"time"
)

// publicScanConfig writes a config for the fake GitHub org in a public
// context, so the privacy guard is active.
func publicScanConfig(t *testing.T) string {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "baseliner.yaml")
	body := "scope:\n  github:\n    type: org\n    name: acme\nprivacy:\n  public_context: true\n"
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// testLogger sets a text logger on buf as the default for the test.
func testLogger(t *testing.T, buf *bytes.Buffer) {
	t.Helper()
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
}

// panicWriter panics on every write, standing in for any panic Scan's own
// code could raise once the scan is under way.
type panicWriter struct{}

func (panicWriter) Write([]byte) (int, error) { panic("write failed for acme/secret-lab") }

// A panic after the privacy guard is installed is recovered, reported through
// the guarded stderr, so it names no private repo, and exits 2. Unrecovered,
// the runtime would print it straight to the process's stderr (#130).
func TestScanRecoversPanicThroughGuard(t *testing.T) {
	srv := fakeGitHub(t)
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "test-token")
	var logs, errb bytes.Buffer
	testLogger(t, &logs)

	code := Scan(panicWriter{}, &errb, Options{ConfigPath: publicScanConfig(t), Format: "table"})
	if code != 2 {
		t.Fatalf("exit = %d, want 2 after a panic\nstderr:\n%s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "internal error") {
		t.Errorf("stderr should report the panic:\n%s", errb.String())
	}
	if strings.Contains(strings.ToLower(errb.String()+logs.String()), "secret-lab") {
		t.Errorf("the panic report names the private repo:\n%s", errb.String())
	}
}

// With slog's built-in default handler still installed, the guard must not
// wrap it: the built-in handler writes through the log package, which
// SetDefault points back at slog, so every record would deadlock (#131).
func TestScanWithBuiltinLoggerDoesNotDeadlock(t *testing.T) {
	srv := fakeGitHub(t)
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "test-token")
	prev := slog.Default()
	slog.SetDefault(slog.New(builtinHandler))
	t.Cleanup(func() { slog.SetDefault(prev) })

	done := make(chan int, 1)
	var out, errb bytes.Buffer
	go func() { done <- Scan(&out, &errb, Options{ConfigPath: publicScanConfig(t), Format: "table"}) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Scan did not finish with slog's built-in default handler installed")
	}
	if strings.Contains(strings.ToLower(errb.String()), "secret-lab") {
		t.Errorf("stderr names the private repo:\n%s", errb.String())
	}
}

// When GitHub rate-limits the scan partway, later checks go unknown. The run
// must say so and exit 2, since the scan could not complete (#132).
func TestScanReportsRateLimit(t *testing.T) {
	healthy, err := url.Parse(fakeGitHub(t).URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(healthy)
	reset := time.Now().Add(30 * time.Minute).Unix()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(strings.ToLower(r.URL.Path), "/repos/acme/secret-lab/") {
			w.Header().Set("X-RateLimit-Limit", "5000")
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "test-token")
	var logs bytes.Buffer
	testLogger(t, &logs)

	code, _, stderr := run(Options{ConfigPath: publicScanConfig(t), Format: "json", FailUnder: fptr(0)})
	if code != 2 {
		t.Fatalf("exit = %d, want 2 after a rate limit\nstderr:\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "rate limit") || !strings.Contains(stderr, time.Unix(reset, 0).UTC().Format("15:04")) {
		t.Errorf("stderr should report the rate limit and when it resets:\n%s", stderr)
	}
	if strings.Contains(strings.ToLower(stderr), "secret-lab") {
		t.Errorf("the rate-limit report names the private repo:\n%s", stderr)
	}
}
