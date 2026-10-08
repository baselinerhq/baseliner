package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baselinerhq/baseliner/internal/privacy"
)

// gitlabPrivateNames are the parts of non-public project paths no sink may
// show in a public context, matched case-insensitively.
var gitlabPrivateNames = []string{"secret-lab", "acme/inside", "acme%2finside", "old-vault", "hidden-x", "team/", "%2fteam"}

// fakeGitLab serves group "acme" (id 1): a public project that passes every
// check, a private one in a subgroup that fails, an internal one that passes,
// an archived private one and a private one the tests exclude. The listing
// spans two pages. fault, when set, answers every request about the private
// project (id 11) instead.
func fakeGitLab(t *testing.T, fault func(w http.ResponseWriter, r *http.Request) bool) *httptest.Server {
	t.Helper()
	recent := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
	good := map[string]string{
		"":                  `[{"path":"README.md","type":"blob","mode":"100644"},{"path":"LICENSE","type":"blob","mode":"100644"},{"path":".gitignore","type":"blob","mode":"100644"},{"path":".gitlab-ci.yml","type":"blob","mode":"100644"}]`,
		".github":           `[{"path":".github/CODEOWNERS","type":"blob","mode":"100644"},{"path":".github/dependabot.yml","type":"blob","mode":"100644"}]`,
		".github/workflows": `[]`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.EscapedPath()
		if fault != nil && strings.HasPrefix(p, "/api/v4/projects/11/") && fault(w, r) {
			return
		}
		project := func(id int, path, vis string, archived bool) string {
			return fmt.Sprintf(`{"id":%d,"path":%q,"path_with_namespace":%q,"visibility":%q,"default_branch":"main","archived":%v,"last_activity_at":%q}`,
				id, path[strings.LastIndex(path, "/")+1:], path, vis, archived, recent)
		}
		switch {
		case strings.EqualFold(p, "/api/v4/groups/acme"):
			_, _ = w.Write([]byte(`{"id":1,"full_path":"acme"}`))
		case p == "/api/v4/groups/1/projects":
			if r.URL.Query().Get("page") == "" {
				w.Header().Set("X-Next-Page", "2")
				_, _ = fmt.Fprintf(w, "[%s,%s]", project(10, "acme/open-kit", "public", false), project(11, "acme/team/secret-lab", "private", false))
				return
			}
			_, _ = fmt.Fprintf(w, "[%s,%s,%s]", project(12, "acme/inside", "internal", false),
				project(13, "acme/old-vault", "private", true), project(14, "acme/team/hidden-x", "private", false))
		case strings.HasSuffix(p, "/repository/tree"):
			id := strings.Split(p, "/")[4]
			if body, ok := good[r.URL.Query().Get("path")]; ok && (id == "10" || id == "12") {
				_, _ = w.Write([]byte(body))
				return
			}
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"404 Tree Not Found"}`))
		case strings.HasSuffix(p, "/repository/files/README.md/raw"):
			_, _ = w.Write([]byte("# Title\n"))
		case strings.HasSuffix(p, "/repository/branches"):
			_, _ = w.Write([]byte(`[{"name":"main"}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"404 Not Found"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

type gitlabScan struct {
	code                  int
	stdout, stderr, logs  string
	json, sarif, markdown string
}

func (s gitlabScan) sinks() map[string]string {
	return map[string]string{"stdout": s.stdout, "stderr": s.stderr, "log": s.logs, "results.json": s.json, "results.sarif": s.sarif, "report.md": s.markdown}
}

// scanGitLab runs a scan of srv's group with the given config body after the
// scope (privacy, policy) and options.
func scanGitLab(t *testing.T, srv *httptest.Server, group, extra string, opts Options) gitlabScan {
	t.Helper()
	t.Setenv("GITLAB_TOKEN", "test-token")
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	dir := t.TempDir()
	cfg := filepath.Join(dir, "baseliner.yaml")
	body := fmt.Sprintf("scope:\n  gitlab:\n    group: %s\n    base_url: %s\n  exclude: [team/hidden-*]\n%s", group, srv.URL, extra)
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out := func(name string) string { return filepath.Join(dir, name) }
	opts.ConfigPath = cfg
	opts.Format = "both"
	opts.OutputFile, opts.SarifFile, opts.MarkdownFile = out("results.json"), out("results.sarif"), out("report.md")
	code, stdout, stderr := run(opts)
	read := func(name string) string {
		b, _ := os.ReadFile(out(name))
		return string(b)
	}
	return gitlabScan{code: code, stdout: stdout, stderr: stderr, logs: logs.String(),
		json: read("results.json"), sarif: read("results.sarif"), markdown: read("report.md")}
}

func assertNoGitLabPrivateNames(t *testing.T, s gitlabScan) {
	t.Helper()
	for sink, text := range s.sinks() {
		low := strings.ToLower(text)
		for _, n := range gitlabPrivateNames {
			if strings.Contains(low, n) {
				t.Errorf("%s names %q:\n%s", sink, n, text)
			}
		}
	}
}

// End to end against a fake GitLab, in each privacy mode, with the group
// spelled as GitLab spells it and otherwise. In a public context no sink
// shows a non-public project's path, nested or encoded; with the guard off
// every sink shows the private one, so the checks are not vacuous.
func TestScanGitLabEndToEnd(t *testing.T) {
	for _, group := range []string{"acme", "ACME"} {
		for _, mode := range []string{"", "redact", "exclude"} {
			t.Run(fmt.Sprintf("group=%s/mode=%s", group, mode), func(t *testing.T) {
				extra := "privacy:\n  public_context: false\n"
				if mode != "" {
					extra = "privacy:\n  public_context: true\n  private_repos: " + mode + "\n"
				}
				s := scanGitLab(t, fakeGitLab(t, nil), group, extra, Options{FailUnder: fptr(0.99)})
				if s.code != 1 || !strings.Contains(s.stderr, "1 repo(s) below --fail-under") {
					t.Fatalf("exit = %d, want 1 with only the private project below --fail-under\nstderr:\n%s", s.code, s.stderr)
				}
				if !strings.Contains(s.stdout, "acme/open-kit") {
					t.Errorf("stdout should show the public project:\n%s", s.stdout)
				}
				switch mode {
				case "":
					// The log is left out: nothing here logs about the private
					// project (a 404 is absence); the fault test covers the log.
					for sink, text := range s.sinks() {
						if sink != "log" && !strings.Contains(strings.ToLower(text), "acme/team/secret-lab") {
							t.Errorf("with the guard off %s should name acme/team/secret-lab:\n%s", sink, text)
						}
					}
					if strings.Contains(s.logs, "old-vault") || strings.Contains(s.logs, "hidden-x") {
						t.Errorf("skipped private projects are never named, even with the guard off:\n%s", s.logs)
					}
				case "redact":
					assertNoGitLabPrivateNames(t, s)
					if !strings.Contains(s.stdout, "private/") || !strings.Contains(s.stderr, privacy.RedactedSlug) {
						t.Errorf("redact mode should show masked rows and a masked --fail-under list:\nstdout:\n%s\nstderr:\n%s", s.stdout, s.stderr)
					}
				case "exclude":
					assertNoGitLabPrivateNames(t, s)
					if !strings.Contains(s.json, `"total_repos": 1`) || !strings.Contains(s.stderr, ": 1 private repo(s)") {
						t.Errorf("exclude mode: totals cover the public project only, and the gate counts the private one:\n%s\n%s", s.json, s.stderr)
					}
					for sink, text := range s.sinks() {
						if strings.Contains(text, privacy.RedactedSlug) {
							t.Errorf("exclude mode: %s still shows a masked project:\n%s", sink, text)
						}
					}
				}
			})
		}
	}
}

// Every call about the private project fails, with a message that quotes its
// path as written and URL-encoded, so every collector warning that can name
// it fires. None may name it in a public context, and in exclude mode none
// may mention it at all.
func TestScanGitLabRedactsEveryAPIFault(t *testing.T) {
	for _, mode := range []string{"redact", "exclude"} {
		for _, status := range []int{http.StatusInternalServerError, http.StatusForbidden, http.StatusNotFound, http.StatusTooManyRequests} {
			t.Run(fmt.Sprintf("%s/%d", mode, status), func(t *testing.T) {
				srv := fakeGitLab(t, func(w http.ResponseWriter, _ *http.Request) bool {
					msg := "failed for acme/team/secret-lab (acme%2Fteam%2Fsecret-lab)"
					if status == http.StatusNotFound {
						msg = "404 Project Not Found: " + msg
					}
					w.WriteHeader(status)
					_, _ = fmt.Fprintf(w, `{"message":%q}`, msg)
					return true
				})
				s := scanGitLab(t, srv, "acme", "privacy:\n  public_context: true\n  private_repos: "+mode+"\n", Options{MinCoverage: fptr(1.0)})
				if s.code == 0 {
					t.Fatalf("exit = 0 although every call about the private project failed\nstderr:\n%s", s.stderr)
				}
				assertNoGitLabPrivateNames(t, s)
				n := strings.Count(s.logs, privacy.RedactedSlug)
				if mode == "redact" && n < 3 {
					t.Errorf("want several masked API faults in the log, got %d:\n%s", n, s.logs)
				}
				if mode == "exclude" && n != 0 {
					t.Errorf("exclude mode: %d masked lines, want none:\n%s", n, s.logs)
				}
			})
		}
	}
}

// In exclude mode a warning about a private project is dropped even when the
// error names no path (GitLab errors carry numeric IDs): the warning carries
// the slug, which is what the guard recognises.
func TestScanGitLabExcludeDropsUnnamedFaults(t *testing.T) {
	srv := fakeGitLab(t, func(w http.ResponseWriter, _ *http.Request) bool {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"boom"}`))
		return true
	})
	s := scanGitLab(t, srv, "acme", "privacy:\n  public_context: true\n  private_repos: exclude\n", Options{})
	if strings.Contains(s.logs, "/projects/11/") || strings.Contains(s.logs, "boom") {
		t.Errorf("exclude mode logged a fault about the private project:\n%s", s.logs)
	}
	// Not vacuous: with the guard off the faults are logged.
	s = scanGitLab(t, srv, "acme", "privacy:\n  public_context: false\n", Options{})
	if !strings.Contains(s.logs, "/projects/11/") {
		t.Errorf("the private project's faults should be logged with the guard off:\n%s", s.logs)
	}
}

// A listing that fails is unread, so the checks that look there are unknown
// and coverage drops; a 404 is absence, so they fail.
func TestScanGitLabUnreadableEvidence(t *testing.T) {
	status := func(code int) func(w http.ResponseWriter, r *http.Request) bool {
		return func(w http.ResponseWriter, r *http.Request) bool {
			if !strings.HasSuffix(r.URL.Path, "/repository/tree") {
				return false
			}
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{"message":"boom"}`))
			return true
		}
	}
	s := scanGitLab(t, fakeGitLab(t, status(http.StatusInternalServerError)), "acme", "", Options{MinCoverage: fptr(1.0)})
	if s.code != 1 || !strings.Contains(s.stderr, "1 repo(s) below --min-coverage 100%: acme/team/secret-lab") {
		t.Errorf("an unreadable tree should leave only that project below --min-coverage:\n%s", s.stderr)
	}
	if got := checkStatus(t, s.json, "acme/team/secret-lab", "readme_exists"); got != "unknown" {
		t.Errorf("readme_exists = %s, want unknown", got)
	}
	s = scanGitLab(t, fakeGitLab(t, nil), "acme", "", Options{})
	if got := checkStatus(t, s.json, "acme/team/secret-lab", "readme_exists"); got != "fail" {
		t.Errorf("readme_exists = %s, want fail when the tree reads as empty", got)
	}
}

func checkStatus(t *testing.T, results, slug, check string) string {
	t.Helper()
	var run struct {
		Repos []struct {
			Slug    string `json:"slug"`
			Results []struct {
				CheckID string `json:"check_id"`
				Status  string `json:"status"`
			} `json:"results"`
		} `json:"repos"`
	}
	if err := json.Unmarshal([]byte(results), &run); err != nil {
		t.Fatalf("results: %v\n%s", err, results)
	}
	for _, r := range run.Repos {
		if r.Slug != slug {
			continue
		}
		for _, c := range r.Results {
			if c.CheckID == check {
				return c.Status
			}
		}
	}
	return "(missing)"
}

// GitLab's rate limit makes the scan incomplete: exit 2, with a count, the
// reset time, and no project named.
func TestScanGitLabRateLimit(t *testing.T) {
	reset := time.Now().Add(time.Hour).UTC()
	srv := fakeGitLab(t, func(w http.ResponseWriter, _ *http.Request) bool {
		w.Header().Set("RateLimit-Reset", fmt.Sprint(reset.Unix()))
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"Retry later"}`))
		return true
	})
	s := scanGitLab(t, srv, "acme", "privacy:\n  public_context: true\n", Options{})
	if s.code != 2 || !strings.Contains(s.stderr, "GitLab refused") || !strings.Contains(s.stderr, reset.Format("2006-01-02 15:04 UTC")) {
		t.Errorf("exit = %d, want 2 with GitLab's rate-limit summary:\n%s", s.code, s.stderr)
	}
	assertNoGitLabPrivateNames(t, s)
}

func TestScanGitLabMissingTokenAndOpenIssues(t *testing.T) {
	srv := fakeGitLab(t, nil)
	s := scanGitLab(t, srv, "acme", "", Options{OpenIssues: true, DryRun: true})
	if s.code != 2 || !strings.Contains(s.stderr, "GitHub repos only") {
		t.Errorf("--open-issues on a GitLab scope: exit %d\n%s", s.code, s.stderr)
	}
	t.Setenv("GITLAB_TOKEN", "")
	dir := t.TempDir()
	cfg := filepath.Join(dir, "baseliner.yaml")
	if err := os.WriteFile(cfg, []byte("scope:\n  gitlab:\n    group: acme\n    base_url: "+srv.URL+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := run(Options{ConfigPath: cfg, Format: "json"})
	if code != 2 || !strings.Contains(stderr, "GitLab token not found in environment variable 'GITLAB_TOKEN'") {
		t.Errorf("missing token: exit %d\n%s", code, stderr)
	}
}

// ignore_when applies to GitLab's internal visibility.
func TestScanGitLabIgnoreWhenInternal(t *testing.T) {
	s := scanGitLab(t, fakeGitLab(t, nil), "acme", "policy:\n  ignore_when:\n    - visibility: [internal]\n      checks: [license_exists]\n", Options{})
	if got := checkStatus(t, s.json, "acme/inside", "license_exists"); got != "(missing)" && got != "skip" {
		t.Errorf("license_exists on the internal project = %s, want it ignored", got)
	}
	if got := checkStatus(t, s.json, "acme/open-kit", "license_exists"); got != "pass" {
		t.Errorf("license_exists on the public project = %s, want pass", got)
	}
}

// A private project's waiver applies, and in a public context its reason is
// not disclosed in any sink.
func TestScanGitLabPrivateWaiverReasonNotDisclosed(t *testing.T) {
	const reason = "ZZQREASON private detail"
	srv := fakeGitLab(t, func(w http.ResponseWriter, r *http.Request) bool {
		switch {
		case strings.HasSuffix(r.URL.Path, "/repository/tree") && r.URL.Query().Get("path") == "":
			_, _ = w.Write([]byte(`[{"path":".baseliner.yml","type":"blob","mode":"100644"}]`))
		case strings.HasSuffix(r.URL.EscapedPath(), "/repository/files/.baseliner.yml/raw"):
			_, _ = fmt.Fprintf(w, "waivers:\n  - check: license_exists\n    reason: %s\n", reason)
		default:
			return false
		}
		return true
	})
	extra := "policy:\n  repo_waivers:\n    allow: [license_exists]\n"
	s := scanGitLab(t, srv, "acme", extra+"privacy:\n  public_context: false\n", Options{})
	if got := checkStatus(t, s.json, "acme/team/secret-lab", "license_exists"); got != "waived" || !strings.Contains(s.json, reason) {
		t.Fatalf("with the guard off the waiver applies and shows its reason: %s\n%s", got, s.json)
	}
	for _, mode := range []string{"redact", "exclude"} {
		s := scanGitLab(t, srv, "acme", extra+"privacy:\n  public_context: true\n  private_repos: "+mode+"\n", Options{})
		for sink, text := range s.sinks() {
			if strings.Contains(text, "ZZQREASON") {
				t.Errorf("%s: %s shows the private waiver reason:\n%s", mode, sink, text)
			}
		}
		assertNoGitLabPrivateNames(t, s)
	}
}

// GitHub and GitLab in one scan: each forge's repos are collected by its own
// collector, and the guard covers both.
func TestScanGitHubAndGitLabTogether(t *testing.T) {
	gh := fakeGitHub(t)
	t.Setenv("GITHUB_API_URL", gh.URL)
	t.Setenv("GITHUB_TOKEN", "test-token")
	t.Setenv("GITLAB_TOKEN", "test-token")
	extra := "  github:\n    type: org\n    name: acme\n"
	dir := t.TempDir()
	cfg := filepath.Join(dir, "baseliner.yaml")
	gl := fakeGitLab(t, nil)
	body := "scope:\n" + extra + "  gitlab:\n    group: acme\n    base_url: " + gl.URL + "\n  exclude: [team/hidden-*]\nprivacy:\n  public_context: true\n"
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)
	out := filepath.Join(dir, "results.json")
	code, stdout, stderr := run(Options{ConfigPath: cfg, Format: "both", OutputFile: out, FailUnder: fptr(0.99)})
	b, _ := os.ReadFile(out)
	if code != 1 || !strings.Contains(stderr, "2 repo(s) below --fail-under") {
		t.Errorf("exit = %d, want 1 with the two private repos below --fail-under:\n%s", code, stderr)
	}
	for sink, text := range map[string]string{"stdout": stdout, "stderr": stderr, "log": logs.String(), "results.json": string(b)} {
		if strings.Contains(strings.ToLower(text), "secret-lab") {
			t.Errorf("%s names secret-lab:\n%s", sink, text)
		}
	}
	// acme/open-kit exists on both forges, and both are scanned.
	if strings.Count(stdout, "acme/open-kit") < 2 || strings.Count(string(b), `"slug": "acme/open-kit"`) != 2 {
		t.Errorf("both forges should be scanned:\n%s\n%s", stdout, b)
	}
}
