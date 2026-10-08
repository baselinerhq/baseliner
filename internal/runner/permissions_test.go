package runner

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A refusal for a missing permission names it, in the error go-github builds
// and so everywhere that error is logged or reported. Anything else passes
// through as GitHub sent it.
func TestPermissionHints(t *testing.T) {
	for name, c := range map[string]struct {
		status   int
		accepted string
		body     string
		want     string // in the error; "" means the body must pass through unchanged
	}{
		"one permission": {http.StatusForbidden, "administration=read",
			`{"message":"Resource not accessible by personal access token","documentation_url":"https://docs.github.com/x","status":"403"}`,
			"Resource not accessible by personal access token (the token needs administration: read)"},
		"alternative sets": {http.StatusForbidden, "pull_requests=read,contents=read; issues=read,contents=read",
			`{"message":"Resource not accessible by integration"}`,
			"(the token needs pull_requests: read and contents: read, or issues: read and contents: read)"},
		"no header":      {http.StatusForbidden, "", `{"message":"API rate limit exceeded"}`, ""},
		"not json":       {http.StatusForbidden, "contents=read", `<html>forbidden</html>`, ""},
		"not a refusal":  {http.StatusNotFound, "contents=read", `{"message":"Not Found"}`, ""},
		"blank header":   {http.StatusForbidden, "  ", `{"message":"x"}`, ""},
		"oversized body": {http.StatusForbidden, "contents=read", `{"message":"` + strings.Repeat("x", maxErrorBody) + `"}`, ""},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if c.accepted != "" {
					w.Header().Set("X-Accepted-GitHub-Permissions", c.accepted)
				}
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()

			// The body as the client sees it, through the transport.
			req, _ := http.NewRequest("GET", srv.URL, nil)
			resp, err := permissionHints{base: http.DefaultTransport}.RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			var got strings.Builder
			_, _ = fmt.Fprint(&got, readAll(t, resp))
			if c.want == "" {
				if got.String() != c.body {
					t.Errorf("body changed: %q", got.String())
				}
				return
			}

			// And the error go-github builds from it.
			t.Setenv("GITHUB_API_URL", srv.URL)
			client, err := newGitHubClient("test-token")
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = client.Repositories.GetBranchProtection(context.Background(), "o", "r", "main")
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %v; want it to contain %q", err, c.want)
			}
		})
	}
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return b.String()
}

// End to end: a token without Administration: Read leaves the classic
// protection check unknown, and the result says which permission to grant.
func TestScanNamesMissingPermission(t *testing.T) {
	healthy, err := url.Parse(fakeGitHub(t).URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(healthy)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/branches/main/protection") {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Accepted-GitHub-Permissions", "administration=read")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"Resource not accessible by personal access token"}`))
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
	dir := t.TempDir()
	cfg := filepath.Join(dir, "baseliner.yaml")
	body := fmt.Sprintf("scope:\n  github:\n    type: org\n    name: acme\npolicy:\n  base: %s\nprivacy:\n  public_context: false\n", policy)
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "results.json")
	_, _, stderr := run(Options{ConfigPath: cfg, Format: "json", OutputFile: out})
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read results: %v\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(string(b), "(the token needs administration: read)") {
		t.Errorf("results do not name the missing permission:\n%s", b)
	}
}
