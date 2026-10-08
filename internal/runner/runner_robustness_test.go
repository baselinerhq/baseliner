package runner

import (
	"bytes"
	"fmt"
	"log"
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

// rateLimitedScan runs a public-context scan against the fake org, with
// respond answering every request about the private repo, and returns the exit
// code and stderr. list, if set, answers the org listing.
func rateLimitedScan(t *testing.T, respond, list http.HandlerFunc) (int, string) {
	t.Helper()
	healthy, err := url.Parse(fakeGitHub(t).URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(healthy)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.ToLower(r.URL.Path)
		switch {
		case list != nil && p == "/orgs/acme/repos":
			list(w, r)
		case strings.Contains(p, "/repos/acme/secret-lab/"):
			respond(w, r)
		default:
			proxy.ServeHTTP(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "test-token")
	var logs bytes.Buffer
	testLogger(t, &logs)
	code, _, stderr := run(Options{ConfigPath: publicScanConfig(t), Format: "json", FailUnder: fptr(0)})
	if strings.Contains(strings.ToLower(stderr+logs.String()), "secret-lab") {
		t.Errorf("output names the private repo:\n%s", stderr)
	}
	return code, stderr
}

// The usual way a limit runs out: a successful response reports no requests
// left, and go-github then refuses every later request itself, without
// sending it. Those refusals must still be counted.
func TestScanReportsLimitSpentBySuccessfulRequest(t *testing.T) {
	reset := time.Now().Add(20 * time.Minute).Unix()
	var reached int
	code, stderr := rateLimitedScan(t,
		func(w http.ResponseWriter, _ *http.Request) { reached++; w.WriteHeader(http.StatusOK) },
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-RateLimit-Limit", "5000")
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))
			_, _ = w.Write([]byte(`[
				{"name":"open-kit","full_name":"acme/open-kit","owner":{"login":"acme"},"visibility":"public","default_branch":"main"},
				{"name":"secret-lab","full_name":"acme/secret-lab","owner":{"login":"acme"},"private":true,"visibility":"private","default_branch":"main"}]`))
		})
	if code != 2 || !strings.Contains(stderr, "GitHub refused") || !strings.Contains(stderr, time.Unix(reset, 0).UTC().Format("15:04")) {
		t.Errorf("exit = %d, want 2 with the refusal and reset reported\nstderr:\n%s", code, stderr)
	}
	if reached != 0 {
		t.Errorf("%d request(s) reached the server after the limit ran out; go-github should refuse them", reached)
	}
}

// A secondary limit can come as a 403 identified only by its documentation
// URL, or as a 429 with Retry-After.
func TestScanReportsSecondaryRateLimits(t *testing.T) {
	for name, respond := range map[string]http.HandlerFunc{
		"403 secondary": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"You have exceeded a secondary rate limit.",` +
				`"documentation_url":"https://docs.github.com/rest/overview/rate-limits-for-the-rest-api#about-secondary-rate-limits"}`))
		},
		"429 Retry-After": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "60")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"message":"Too Many Requests"}`))
		},
	} {
		t.Run(name, func(t *testing.T) {
			if code, stderr := rateLimitedScan(t, respond, nil); code != 2 || !strings.Contains(stderr, "GitHub refused") {
				t.Errorf("exit = %d, want 2 with the refusal reported\nstderr:\n%s", code, stderr)
			} else if name == "429 Retry-After" && !strings.Contains(stderr, "resets at") {
				t.Errorf("a 429's Retry-After should give the reset time:\n%s", stderr)
			}
		})
	}
}

// A handler derived from the built-in one with With is the same kind of
// handler and deadlocks just the same if wrapped.
func TestScanWithDerivedBuiltinLoggerDoesNotDeadlock(t *testing.T) {
	srv := fakeGitHub(t)
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "test-token")
	prev := slog.Default()
	slog.SetDefault(slog.New(builtinHandler).With("app", "x"))
	t.Cleanup(func() { slog.SetDefault(prev) })
	done := make(chan int, 1)
	var out, errb bytes.Buffer
	go func() { done <- Scan(&out, &errb, Options{ConfigPath: publicScanConfig(t), Format: "table"}) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Scan did not finish with a handler derived from the built-in one")
	}
}

// With the built-in handler in place, the guard logs to the given stderr,
// redacted, and afterwards hands the log package its own output back.
func TestScanWithBuiltinLoggerLogsRedactedAndRestoresLog(t *testing.T) {
	srv := fakeGitHub(t)
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "test-token")
	prev := slog.Default()
	slog.SetDefault(slog.New(builtinHandler))
	t.Cleanup(func() { slog.SetDefault(prev) })
	logOut, logFlags := log.Writer(), log.Flags() //nolint:forbidigo // checking it is restored

	var out, errb bytes.Buffer
	Scan(&out, &errb, Options{ConfigPath: publicScanConfig(t), Format: "table"})
	if !strings.Contains(errb.String(), "private/redacted") || strings.Contains(errb.String(), "secret-lab") {
		t.Errorf("the scan's log lines should reach stderr redacted:\n%s", errb.String())
	}
	if log.Writer() != logOut || log.Flags() != logFlags { //nolint:forbidigo // checking it is restored
		t.Errorf("log package left redirected after Scan: writer %T flags %d, want %T %d", log.Writer(), log.Flags(), logOut, logFlags) //nolint:forbidigo // checking it is restored
	}
}

// A rate limit while delivering findings issues is reported too, in a real
// run and in a dry run, which still searches for existing issues.
func TestScanReportsRateLimitDuringIssueDelivery(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(fmt.Sprintf("dry_run=%v", dryRun), func(t *testing.T) {
			healthy, err := url.Parse(fakeGitHub(t).URL)
			if err != nil {
				t.Fatal(err)
			}
			proxy := httputil.NewSingleHostReverseProxy(healthy)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/issues") && r.Method == http.MethodGet {
					w.Header().Set("X-RateLimit-Remaining", "0")
					w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
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
			code, _, stderr := run(Options{ConfigPath: publicScanConfig(t), Format: "json", FailUnder: fptr(0), OpenIssues: true, DryRun: dryRun})
			if code != 2 || !strings.Contains(stderr, "GitHub refused") {
				t.Errorf("exit = %d, want 2 with the refusal reported\nstderr:\n%s", code, stderr)
			}
		})
	}
}
