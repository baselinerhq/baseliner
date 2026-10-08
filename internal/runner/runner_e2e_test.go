package runner

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		_, _ = w.Write([]byte(`[
			{"name":"open-kit","full_name":"acme/open-kit","owner":{"login":"acme"},"visibility":"public","default_branch":"main"},
			{"name":"secret-lab","full_name":"acme/secret-lab","owner":{"login":"acme"},"private":true,"visibility":"private","default_branch":"main"}
		]`))
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
// writes that a dry run never makes.
func TestScanPublicContextEndToEnd(t *testing.T) {
	const privateName = "secret-lab"
	for _, org := range []string{"acme", "ACME"} {
		for _, dryRun := range []bool{true, false} {
			for _, public := range []bool{true, false} {
				name := fmt.Sprintf("org=%s/dry_run=%v/public_context=%v", org, dryRun, public)
				t.Run(name, func(t *testing.T) {
					scanPublicContext(t, org, dryRun, public, privateName)
				})
			}
		}
	}
}

func scanPublicContext(t *testing.T, org string, dryRun, public bool, privateName string) {
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
		if !strings.Contains(strings.ToLower(stdout), "acme/open-kit") || !strings.Contains(stdout, "private/1") {
			t.Errorf("stdout should show the public repo and a redacted row:\n%s", stdout)
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
