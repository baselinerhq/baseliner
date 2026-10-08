package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baselinerhq/baseliner/internal/privacy"
)

// fakeGitHub serves an org with one public and one private repo. The public
// repo passes every check, so only the private repo is below a --fail-under.
// Everything else 404s: the collector reads that as "not present", so the
// private repo fails its checks. The issue search finds nothing, and creating
// the label or the issue is denied, so a real --open-issues run reports a
// delivery failure for the private repo, the path that leaked in #84. Both
// produce log lines naming the repos, in URLs that use GitHub's spelling of
// the owner ("acme"), whatever the config says. Routing ignores case, as
// GitHub's does.
func fakeGitHub(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rate_limit", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"resources":{"core":{"limit":5000,"remaining":5000,"reset":0}}}`))
	})
	listings := map[string]string{
		"/repos/acme/open-kit/contents/": `[{"type":"file","path":"README.md"},` +
			`{"type":"file","path":"LICENSE"},{"type":"file","path":".gitignore"}]`,
		"/repos/acme/open-kit/contents/.github": `[{"type":"file","path":".github/CODEOWNERS"},` +
			`{"type":"file","path":".github/dependabot.yml"}]`,
		"/repos/acme/open-kit/contents/.github/workflows": `[{"type":"file","path":".github/workflows/ci.yml"}]`,
	}
	mux.HandleFunc("GET /repos/acme/open-kit/contents/", func(w http.ResponseWriter, r *http.Request) {
		if body, ok := listings[r.URL.Path]; ok {
			_, _ = w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("GET /repos/acme/open-kit/readme", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"encoding":"base64","content":"IyBUaXRsZQ=="}`)) // "# Title"
	})
	mux.HandleFunc("GET /repos/acme/open-kit/branches", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"name":"main"}]`))
	})
	mux.HandleFunc("GET /repos/acme/{repo}/issues", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	denied := func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"Resource not accessible by personal access token"}`, http.StatusForbidden)
	}
	mux.HandleFunc("POST /repos/acme/{repo}/labels", denied)
	mux.HandleFunc("POST /repos/acme/{repo}/issues", denied)
	mux.HandleFunc("GET /orgs/acme/repos", func(w http.ResponseWriter, _ *http.Request) {
		pushed := time.Now().UTC().Format(time.RFC3339)
		_, _ = fmt.Fprintf(w, `[
			{"name":"open-kit","full_name":"acme/open-kit","owner":{"login":"acme"},"visibility":"public","default_branch":"main","pushed_at":%q},
			{"name":"secret-lab","full_name":"acme/secret-lab","owner":{"login":"acme"},"private":true,"visibility":"private","default_branch":"main","pushed_at":%q}
		]`, pushed, pushed)
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path, r.URL.RawPath = strings.ToLower(r.URL.Path), ""
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Drives Scan end to end against a fake GitHub with every output enabled. In a
// public context the private repo must not be named in any sink — stdout,
// stderr, the log, JSON, SARIF, Markdown — in any spelling, while the gate
// still counts its failures: it is the only repo below --fail-under. With the
// guard off every sink names it, which shows each check can see a leak.
//
// It runs over the ways the private repo's name reaches a log line: the
// config's spelling of the org can differ in case from GitHub's, which API
// URLs use, and a real --open-issues run logs the denied label and issue
// writes that a dry run never makes. In a public context it runs in both
// protecting modes that write output: redact masks the private repo, and
// exclude must leave no trace of it beyond a count.
func TestScanPublicContextEndToEnd(t *testing.T) {
	const privateName = "secret-lab"
	for _, org := range []string{"acme", "ACME"} {
		for _, dryRun := range []bool{true, false} {
			for _, mode := range []string{"", "redact", "exclude"} {
				public := mode != ""
				name := fmt.Sprintf("org=%s/dry_run=%v/public_context=%v", org, dryRun, public)
				if public {
					name += "/mode=" + mode
				}
				t.Run(name, func(t *testing.T) {
					scanPublicContext(t, org, dryRun, mode, privateName)
				})
			}
		}
	}
}

// scanPublicContext runs the scan with mode as privacy.private_repos in a
// public context, or with the guard off when mode is "".
func scanPublicContext(t *testing.T, org string, dryRun bool, mode, privateName string) {
	public := mode != ""
	srv := fakeGitHub(t)
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "test-token")

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	dir := t.TempDir()
	cfg := filepath.Join(dir, "baseliner.yaml")
	body := fmt.Sprintf("scope:\n  github:\n    type: org\n    name: %s\nprivacy:\n  public_context: %v\n", org, public)
	if public {
		body += "  private_repos: " + mode + "\n"
	}
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out := func(name string) string { return filepath.Join(dir, name) }

	code, stdout, stderr := run(Options{
		ConfigPath:   cfg,
		Format:       "both",
		OutputFile:   out("results.json"),
		SarifFile:    out("results.sarif"),
		MarkdownFile: out("report.md"),
		OpenIssues:   true,
		DryRun:       dryRun,
		FailUnder:    fptr(0.99),
	})
	// A dry run makes no writes, so only the private repo's --fail-under
	// failure counts. A real run is denied the private repo's label, and exit 2
	// outranks that failure, whose list is still printed.
	want := 1
	if !dryRun {
		want = 2
	}
	if code != want {
		t.Fatalf("exit = %d, want %d\nstderr:\n%s", code, want, stderr)
	}
	if !strings.Contains(stderr, "1 repo(s) below --fail-under") {
		t.Errorf("only the private repo should be below --fail-under:\n%s", stderr)
	}
	if mode == "exclude" && !strings.Contains(stderr, ": 1 private repo(s)") {
		t.Errorf("exclude mode should count the private repo in the --fail-under list, without a name or score:\n%s", stderr)
	}

	sinks := map[string]string{"stdout": stdout, "stderr": stderr, "log": logs.String()}
	for _, f := range []string{"results.json", "results.sarif", "report.md"} {
		b, err := os.ReadFile(out(f))
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		sinks[f] = string(b)
	}

	if public {
		// The bare name in any case covers every slug spelling too: a log line
		// that drops the owner, or spells it as GitHub does, still names it.
		for sink, s := range sinks {
			if strings.Contains(strings.ToLower(s), privateName) {
				t.Errorf("%s names %s:\n%s", sink, privateName, s)
			}
		}
		if !strings.Contains(strings.ToLower(stdout), "acme/open-kit") {
			t.Errorf("stdout should show the public repo:\n%s", stdout)
		}
		if mode == "exclude" {
			// The totals cover only the disclosed repo (#91).
			if !strings.Contains(sinks["results.json"], `"total_repos": 1`) || !strings.Contains(stdout, "1 repos scanned") {
				t.Errorf("exclude mode: totals should cover only the public repo:\n%s\n%s", stdout, sinks["results.json"])
			}
			// The private repo's delivery warning was dropped, so the summary
			// must say why it points at nothing.
			if !dryRun && !strings.Contains(stderr, "warnings about private repos are not logged") {
				t.Errorf("exclude mode: the delivery summary should say private repos' warnings are omitted:\n%s", stderr)
			}
			// Absent, not masked: a private/redacted line or a private/1 row
			// still shows the repo exists and how it scored.
			for sink, s := range sinks {
				if strings.Contains(s, privacy.RedactedSlug) || strings.Contains(s, "private/1") {
					t.Errorf("exclude mode: %s still shows the private repo:\n%s", sink, s)
				}
			}
			return
		}
		if !strings.Contains(stdout, "private/1") {
			t.Errorf("stdout should show a redacted row:\n%s", stdout)
		}
		// Not vacuous: the log and stderr did carry lines about the private
		// repo (issue lookup or write, --fail-under list), masked.
		for sink, s := range map[string]string{"log": logs.String(), "stderr": stderr} {
			if !strings.Contains(s, privacy.RedactedSlug) {
				t.Errorf("%s has no redacted slug, so it never mentioned the private repo:\n%s", sink, s)
			}
		}
		return
	}
	// With the guard off every sink names the private repo, so none of the
	// "not named" checks above can pass on an empty sink.
	for sink, s := range sinks {
		if !strings.Contains(strings.ToLower(s), "acme/"+privateName) {
			t.Errorf("with the guard off %s should name acme/%s:\n%s", sink, privateName, s)
		}
	}
	// A real run's log carries the denied write's URL, in GitHub's spelling,
	// so the public-context case above checks a line that spelling reaches.
	if !dryRun && !strings.Contains(logs.String(), "repos/acme/"+privateName+"/labels") {
		t.Errorf("a real run should log the denied label write:\n%s", logs.String())
	}
}

// The test above reaches only the log lines its fake happens to trigger, and
// #84 and the mixed-case leak were both on lines no test had triggered. Here
// every API call about the private repo fails, with an error message that
// quotes the request path as GitHub's messages can quote a repo, so every
// collector and issue warning that can name it fires at once, with the
// opt-in forge-control checks on. None may name it in a public context, and in
// exclude mode none may mention it at all.
//
// It runs over each protecting mode that writes output, a server error, a 403,
// and the 404 GitHub returns for a private repo the token cannot see, with the
// org spelled as GitHub spells it and in a different case, so that no
// combination escapes the guard.
func TestScanPublicContextRedactsEveryAPIFault(t *testing.T) {
	for _, mode := range []string{"redact", "exclude"} {
		for _, status := range []int{http.StatusInternalServerError, http.StatusForbidden, http.StatusNotFound} {
			for _, org := range []string{"acme", "ACME"} {
				t.Run(fmt.Sprintf("mode=%s/status=%d/org=%s", mode, status, org), func(t *testing.T) {
					scanWithAPIFaults(t, mode, status, org)
				})
			}
		}
	}
}

func scanWithAPIFaults(t *testing.T, mode string, status int, org string) {
	const privateName = "secret-lab"
	healthy, err := url.Parse(fakeGitHub(t).URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(healthy)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(strings.ToLower(r.URL.Path), privateName) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = fmt.Fprintf(w, `{"message":"failed on %s"}`, r.URL.Path)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "test-token")

	policy, err := filepath.Abs("../../examples/policies/forge-controls.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	dir := t.TempDir()
	cfg := filepath.Join(dir, "baseliner.yaml")
	body := fmt.Sprintf("scope:\n  github:\n    type: org\n    name: %s\npolicy:\n  base: %s\n"+
		"privacy:\n  public_context: true\n  private_repos: %s\n", org, policy, mode)
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out := func(name string) string { return filepath.Join(dir, name) }
	code, stdout, stderr := run(Options{
		ConfigPath:   cfg,
		Format:       "both",
		OutputFile:   out("results.json"),
		SarifFile:    out("results.sarif"),
		MarkdownFile: out("report.md"),
		OpenIssues:   true,
		MinCoverage:  fptr(1.0),
	})
	if code == 0 {
		t.Fatalf("exit = 0 although every call about the private repo failed\nstderr:\n%s", stderr)
	}

	sinks := map[string]string{"stdout": stdout, "stderr": stderr, "log": logs.String()}
	for _, f := range []string{"results.json", "results.sarif", "report.md"} {
		b, err := os.ReadFile(out(f))
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		sinks[f] = string(b)
	}
	for sink, s := range sinks {
		if strings.Contains(strings.ToLower(s), privateName) {
			t.Errorf("%s names %s:\n%s", sink, privateName, s)
		}
	}
	if mode == "exclude" {
		for sink, s := range sinks {
			if strings.Contains(s, privacy.RedactedSlug) || strings.Contains(s, "private/1") {
				t.Errorf("exclude mode: %s still shows the private repo:\n%s", sink, s)
			}
		}
		// Both repos are below 100%; the private one is counted, not named,
		// and still counts toward the total.
		if !strings.Contains(stderr, "2 repo(s) below --min-coverage 100%: "+org+"/open-kit (0%), 1 private repo(s)") {
			t.Errorf("exclude mode: the --min-coverage list should count the private repo:\n%s", stderr)
		}
		return
	}
	// Not vacuous: the faults did reach the log, as redacted URLs.
	if n := strings.Count(logs.String(), "repos/"+privacy.RedactedSlug+"/"); n < 3 {
		t.Errorf("want several redacted API faults in the log, got %d:\n%s", n, logs.String())
	}
}

// Under GitHub Actions the run log and artifacts are public whenever the repo
// is, so a scan that sets no public context must protect private repos and say
// why, and only an explicit false, in config or flag, turns the guard off.
func TestScanUnderGitHubActionsFailsClosed(t *testing.T) {
	const privateName = "secret-lab"
	fls := false
	for _, c := range []struct {
		name       string
		privacy    string
		flag       *bool
		wantHidden bool
	}{
		{"nothing set", "", nil, true},
		{"config false", "privacy:\n  public_context: false\n", nil, false},
		{"flag false", "", &fls, false},
		// allow discloses private repos even in a public context, so a notice
		// saying the guard is on would be false.
		{"allow mode", "privacy:\n  private_repos: allow\n", nil, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := fakeGitHub(t)
			t.Setenv("GITHUB_API_URL", srv.URL)
			t.Setenv("GITHUB_TOKEN", "test-token")
			var logs bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
			defer slog.SetDefault(prev)
			cfg := filepath.Join(t.TempDir(), "baseliner.yaml")
			body := "scope:\n  github:\n    type: org\n    name: acme\n" + c.privacy
			if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			_, stdout, stderr := run(Options{ConfigPath: cfg, Format: "table", PublicContext: c.flag, GitHubActions: true})

			named := strings.Contains(stdout+stderr+logs.String(), privateName)
			notice := strings.Count(stderr, "privacy guard on: running under GitHub Actions") == 1
			if named == c.wantHidden || notice != c.wantHidden {
				t.Errorf("private repo named=%v, notice=%v; want named=%v, notice=%v\nstdout:\n%s\nstderr:\n%s",
					named, notice, !c.wantHidden, c.wantHidden, stdout, stderr)
			}
		})
	}
}

// The ci_present fallback warning fires once per run, for whichever repo's
// workflow listing fails first, and it is about every repo's results. In
// exclude mode it must still appear when that first repo is private, so it
// must not name the repo. The public repo's listing is delayed so the private
// one always fails first.
func TestScanExcludeKeepsWorkflowFallbackWarning(t *testing.T) {
	healthy, err := url.Parse(fakeGitHub(t).URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(healthy)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.ToLower(r.URL.Path)
		if p == "/repos/acme/secret-lab/contents/.github/workflows" {
			_, _ = w.Write([]byte(`[{"type":"file","path":".github/workflows/ci.yml"}]`))
			return
		}
		if strings.HasSuffix(p, "/actions/workflows") {
			if strings.Contains(p, "open-kit") {
				time.Sleep(200 * time.Millisecond)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"Resource not accessible by integration"}`))
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "test-token")

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)
	cfg := filepath.Join(t.TempDir(), "baseliner.yaml")
	body := "scope:\n  github:\n    type: org\n    name: acme\nprivacy:\n  public_context: true\n  private_repos: exclude\n"
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	run(Options{ConfigPath: cfg, Format: "json"})
	out := logs.String()
	if !strings.Contains(out, "ci_present falls back to file presence") {
		t.Errorf("the fallback warning was dropped with the private repo's record:\n%s", out)
	}
	if strings.Contains(strings.ToLower(out), "secret-lab") {
		t.Errorf("the log names the private repo:\n%s", out)
	}
}

// Under the default gate the table is the explanation for a red run, and in
// exclude mode it hides the private repo. A run that fails only because of
// that repo must still say why, by count.
func TestScanExcludeExplainsDefaultGateFailure(t *testing.T) {
	srv := fakeGitHub(t)
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "test-token")
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(prev)
	cfg := filepath.Join(t.TempDir(), "baseliner.yaml")
	body := "scope:\n  github:\n    type: org\n    name: acme\nprivacy:\n  public_context: true\n  private_repos: exclude\n"
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := run(Options{ConfigPath: cfg, Format: "table"})
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (the private repo fails its checks)", code)
	}
	if !strings.Contains(stderr, "1 private repo(s) failed") {
		t.Errorf("a red run should say a private repo failed:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if strings.Contains(strings.ToLower(stdout+stderr+logs.String()), "secret-lab") {
		t.Errorf("names the private repo:\n%s\n%s", stdout, stderr)
	}
}

func TestNewGitHubClientAPIURL(t *testing.T) {
	for _, c := range []struct {
		env, want string
		wantErr   bool
	}{
		{env: "", want: "https://api.github.com/"},
		{env: "https://ghes.example.com/api/v3", want: "https://ghes.example.com/api/v3/"},
		{env: "https://ghes.example.com/api/v3/", want: "https://ghes.example.com/api/v3/"},
		{env: "ghes.example.com/api/v3", wantErr: true},
		{env: "://bad", wantErr: true},
		{env: "https:/api/v3", wantErr: true}, // absolute, but no host
	} {
		t.Run(c.env, func(t *testing.T) {
			t.Setenv("GITHUB_API_URL", c.env)
			client, err := newGitHubClient("t")
			if c.wantErr {
				if err == nil {
					t.Fatalf("want error for %q, got BaseURL %s", c.env, client.BaseURL)
				}
				return
			}
			if err != nil {
				t.Fatalf("newGitHubClient: %v", err)
			}
			if got := client.BaseURL.String(); got != c.want {
				t.Errorf("BaseURL = %s, want %s", got, c.want)
			}
		})
	}
}

// policy.ignore_when waives a check on every repo of a visibility without
// naming one: here license_exists on private repos. The private repo is no
// longer judged on it; the public repo still is.
func TestScanIgnoreWhenVisibility(t *testing.T) {
	srv := fakeGitHub(t)
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "test-token")
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(prev)
	dir := t.TempDir()
	cfg := filepath.Join(dir, "baseliner.yaml")
	body := "scope:\n  github:\n    type: org\n    name: acme\npolicy:\n  ignore_when:\n" +
		"    - visibility: [private]\n      checks: [license_exists]\n"
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "results.json")
	run(Options{ConfigPath: cfg, Format: "json", OutputFile: out})
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Repos []struct {
			Slug    string `json:"slug"`
			Results []struct {
				CheckID string `json:"check_id"`
			} `json:"results"`
		} `json:"repos"`
	}
	if err := json.Unmarshal(b, &res); err != nil {
		t.Fatal(err)
	}
	checked := map[string]bool{}
	for _, r := range res.Repos {
		for _, c := range r.Results {
			if c.CheckID == "license_exists" {
				checked[r.Slug] = true
			}
		}
	}
	if checked["acme/secret-lab"] || !checked["acme/open-kit"] {
		t.Errorf("license_exists checked on %v, want acme/open-kit only", checked)
	}
}

// A scan whose ignore_when names a check that does not exist stops with a
// config error rather than running with a waiver that applies to nothing.
func TestScanRejectsUnknownIgnoreWhenCheck(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "baseliner.yaml")
	body := "scope:\n  local:\n    paths: [\"" + t.TempDir() + "\"]\npolicy:\n  ignore_when:\n" +
		"    - visibility: [private]\n      checks: [licence_exists]\n"
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := run(Options{ConfigPath: cfg, Format: "json"})
	if code != 2 || !strings.Contains(stderr, `unknown check "licence_exists"`) {
		t.Errorf("exit = %d, want 2 naming the unknown check\nstderr:\n%s", code, stderr)
	}
}

// A repo's own .baseliner.yml waives a check the policy allows: the check is
// reported as waived, with the repo's reason, in JSON and Markdown, and no
// longer fails the run.
func TestScanRepoWaiver(t *testing.T) {
	repo := t.TempDir()
	for f, body := range map[string]string{
		"README.md": "# Title\n", "LICENSE": "x", ".gitignore": "x", ".github/CODEOWNERS": "x", ".github/dependabot.yml": "x",
		".baseliner.yml": "waivers:\n  - check: ci_present\n    reason: docs only, nothing to build\n",
	} {
		p := filepath.Join(repo, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	scan := func(allow string) (int, string, string) {
		cfg := filepath.Join(dir, "baseliner.yaml")
		body := "scope:\n  local:\n    paths: [\"" + repo + "\"]\npolicy:\n  ignore: [default_branch_is_main, stale_repo]\n" + allow
		if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		code, _, _ := run(Options{ConfigPath: cfg, Format: "json", OutputFile: filepath.Join(dir, "r.json"), MarkdownFile: filepath.Join(dir, "r.md")})
		j, _ := os.ReadFile(filepath.Join(dir, "r.json"))
		md, _ := os.ReadFile(filepath.Join(dir, "r.md"))
		return code, string(j), string(md)
	}
	if code, _, _ := scan(""); code != 1 {
		t.Fatalf("without policy.repo_waivers the missing CI should fail the run: exit %d", code)
	}
	code, j, md := scan("  repo_waivers:\n    allow: [ci_present]\n")
	if code != 0 {
		t.Errorf("exit = %d, want 0 with ci_present waived", code)
	}
	if !strings.Contains(j, `"status": "waived"`) || !strings.Contains(j, "docs only, nothing to build") {
		t.Errorf("JSON should report ci_present as waived with the reason:\n%s", j)
	}
	if !strings.Contains(md, "waived") || !strings.Contains(md, "docs only, nothing to build") {
		t.Errorf("Markdown should show the waiver:\n%s", md)
	}
}

// A private repo's waiver file is text from inside that repo, so in a public
// context none of it may reach a sink, in either protecting mode: not a valid
// waiver's reason, not a check name that is not a check, not a value an
// invalid file's error would quote.
func TestScanPrivateWaiverReasonNotDisclosed(t *testing.T) {
	const reason = "ZZQ-PRIVATE-REASON"
	files := map[string]string{
		"valid":         "waivers:\n  - check: license_exists\n    reason: " + reason + "\n",
		"not a check":   "waivers:\n  - check: " + reason + "\n    reason: x\n",
		"invalid value": "waivers:\n  - check: license_exists\n    reason: x\n    until: " + reason + "\n",
		"unknown key":   "waivers:\n  - check: license_exists\n    reason: x\n    " + reason + ": 1\n",
	}
	for _, mode := range []string{"redact", "exclude"} {
		for name, content := range files {
			t.Run(mode+"/"+name, func(t *testing.T) { scanPrivateWaiverFile(t, mode, content, reason, name == "valid") })
		}
	}
}

func scanPrivateWaiverFile(t *testing.T, mode, content, reason string, valid bool) {
	{
		{
			healthy, err := url.Parse(fakeGitHub(t).URL)
			if err != nil {
				t.Fatal(err)
			}
			proxy := httputil.NewSingleHostReverseProxy(healthy)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch strings.ToLower(r.URL.Path) {
				case "/repos/acme/secret-lab/contents/":
					_, _ = w.Write([]byte(`[{"type":"file","name":".baseliner.yml","path":".baseliner.yml"}]`))
				case "/repos/acme/secret-lab/contents/.baseliner.yml":
					_, _ = w.Write([]byte(`{"type":"file","sha":"w1","size":` + fmt.Sprint(len(content)) + `}`))
				case "/repos/acme/secret-lab/git/blobs/w1":
					_, _ = w.Write([]byte(content))
				default:
					proxy.ServeHTTP(w, r)
				}
			}))
			t.Cleanup(srv.Close)
			t.Setenv("GITHUB_API_URL", srv.URL)
			t.Setenv("GITHUB_TOKEN", "test-token")
			var logs bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
			defer slog.SetDefault(prev)
			dir := t.TempDir()
			cfg := filepath.Join(dir, "baseliner.yaml")
			body := "scope:\n  github:\n    type: org\n    name: acme\npolicy:\n  repo_waivers:\n    allow: [license_exists]\n" +
				"privacy:\n  public_context: true\n  private_repos: " + mode + "\n"
			if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			out := func(n string) string { return filepath.Join(dir, n) }
			_, stdout, stderr := run(Options{ConfigPath: cfg, Format: "both", OutputFile: out("r.json"), SarifFile: out("r.sarif"), MarkdownFile: out("r.md")})
			sinks := map[string]string{"stdout": stdout, "stderr": stderr, "log": logs.String()}
			for _, f := range []string{"r.json", "r.sarif", "r.md"} {
				b, _ := os.ReadFile(out(f))
				sinks[f] = string(b)
			}
			for sink, s := range sinks {
				if strings.Contains(s, reason) || strings.Contains(strings.ToLower(s), "secret-lab") {
					t.Errorf("%s discloses the private repo's waiver:\n%s", sink, s)
				}
			}
			// Not vacuous: in redact mode the waiver was read and applied,
			// shown as waived with its reason blanked.
			if valid && mode == "redact" && !strings.Contains(sinks["r.json"], `"status": "waived"`) {
				t.Errorf("the private repo's waiver was not applied:\n%s", sinks["r.json"])
			}
		}
	}
}

// Where the org listing says the token's user cannot push, --open-issues does
// not create a findings issue (GitHub would drop its label) and reports the
// delivery failure.
func TestScanOpenIssuesRefusesWithoutPush(t *testing.T) {
	healthy, err := url.Parse(fakeGitHub(t).URL)
	if err != nil {
		t.Fatal(err)
	}
	var created bool
	proxy := httputil.NewSingleHostReverseProxy(healthy)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.EqualFold(r.URL.Path, "/orgs/acme/repos"):
			pushed := time.Now().UTC().Format(time.RFC3339)
			_, _ = fmt.Fprintf(w, `[{"name":"secret-lab","full_name":"acme/secret-lab","owner":{"login":"acme"},"visibility":"public","default_branch":"main","pushed_at":%q,"permissions":{"pull":true,"triage":true,"push":false}}]`, pushed)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/issues"):
			created = true
			http.Error(w, `{"message":"unexpected"}`, http.StatusInternalServerError)
		default:
			proxy.ServeHTTP(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "test-token")
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(prev)
	cfg := filepath.Join(t.TempDir(), "baseliner.yaml")
	if err := os.WriteFile(cfg, []byte("scope:\n  github:\n    type: org\n    name: acme\nprivacy:\n  public_context: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := run(Options{ConfigPath: cfg, Format: "json", OpenIssues: true})
	if code != 2 || created || !strings.Contains(logs.String(), "lacks push access") {
		t.Errorf("exit = %d, created = %v; want 2, no issue, and the reason logged\nstderr:\n%s\nlog:\n%s", code, created, stderr, logs.String())
	}
}
