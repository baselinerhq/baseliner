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
// private repo fails its checks, and the issue search fails for both repos, so
// --open-issues reports a delivery failure. Both produce log lines naming the
// repos.
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
	mux.HandleFunc("GET /orgs/acme/repos", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[
			{"name":"open-kit","owner":{"login":"acme"},"visibility":"public","default_branch":"main"},
			{"name":"secret-lab","owner":{"login":"acme"},"private":true,"visibility":"private","default_branch":"main"}
		]`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// Drives Scan end to end against a fake GitHub with every output enabled. In a
// public context the private repo must not be named in any sink — stdout,
// stderr, the log, JSON, SARIF, Markdown — by slug or bare name, while the gate
// still counts its failures: it is the only repo below --fail-under. With the
// guard off every sink names it, which shows each check can see a leak.
func TestScanPublicContextEndToEnd(t *testing.T) {
	const private, privateName = "acme/secret-lab", "secret-lab"
	for _, public := range []bool{true, false} {
		t.Run(fmt.Sprintf("public_context=%v", public), func(t *testing.T) {
			srv := fakeGitHub(t)
			t.Setenv("GITHUB_API_URL", srv.URL)
			t.Setenv("GITHUB_TOKEN", "test-token")

			var logs bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
			defer slog.SetDefault(prev)

			dir := t.TempDir()
			cfg := filepath.Join(dir, "baseliner.yaml")
			body := fmt.Sprintf("scope:\n  github:\n    type: org\n    name: acme\nprivacy:\n  public_context: %v\n", public)
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
				DryRun:       true,
				FailUnder:    fptr(0.99),
			})
			// The fake 404s the issue search, and even a dry run searches for an
			// existing issue (a read), so delivery fails: exit 2 outranks the
			// private repo's --fail-under failure, whose list is still printed.
			if code != 2 {
				t.Fatalf("exit = %d, want 2 (the issue search fails)\nstderr:\n%s", code, stderr)
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
				// The bare name covers the slug too: a log line that drops the
				// owner still names the repo.
				for sink, s := range sinks {
					if strings.Contains(s, privateName) {
						t.Errorf("%s names %s:\n%s", sink, privateName, s)
					}
				}
				if !strings.Contains(stdout, "acme/open-kit") || !strings.Contains(stdout, "private/1") {
					t.Errorf("stdout should show the public repo and a redacted row:\n%s", stdout)
				}
				// Not vacuous: the log and stderr did carry lines about the
				// private repo (issue lookup, --fail-under list), masked.
				for sink, s := range map[string]string{"log": logs.String(), "stderr": stderr} {
					if !strings.Contains(s, privacy.RedactedSlug) {
						t.Errorf("%s has no redacted slug, so it never mentioned the private repo:\n%s", sink, s)
					}
				}
				return
			}
			// With the guard off every sink names the private repo, so none of
			// the "not named" checks above can pass on an empty sink.
			for sink, s := range sinks {
				if !strings.Contains(s, private) {
					t.Errorf("with the guard off %s should name %s:\n%s", sink, private, s)
				}
			}
		})
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
