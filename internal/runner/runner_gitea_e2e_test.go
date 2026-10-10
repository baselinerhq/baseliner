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
	"time"

	"github.com/baselinerhq/baseliner/internal/privacy"
)

// giteaPrivateNames are the non-public repos' names no sink may show in a
// public context, matched case-insensitively.
var giteaPrivateNames = []string{"secret-lab", "old-vault", "looks-public"}

// fakeGitea serves org "acme" in the shapes Forgejo 16 returns: a public repo
// that passes every check, a private one with a waiver, a repo that is public
// by its own flags but owned by a limited organisation (so not public), and
// an archived private one. fault, when set, answers every request about a
// non-public repo instead.
func fakeGitea(t *testing.T, fault func(w http.ResponseWriter, r *http.Request) bool) *httptest.Server {
	t.Helper()
	recent := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
	good := map[string]string{
		"":                   `[{"path":"README.md","type":"file"},{"path":"LICENSE","type":"file"},{"path":".gitignore","type":"file"}]`,
		".github":            `[{"path":".github/CODEOWNERS","type":"file"},{"path":".github/dependabot.yml","type":"file"}]`,
		".forgejo/workflows": `[{"path":".forgejo/workflows/ci.yml","type":"file"}]`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.ToLower(r.URL.Path)
		if fault != nil && !strings.Contains(p, "/repos/acme/open-kit/") && strings.HasPrefix(p, "/api/v1/repos/") && fault(w, r) {
			return
		}
		repo := func(name, ownerVis string, private, archived bool) string {
			return fmt.Sprintf(`{"name":%q,"full_name":"acme/%s","owner":{"login":"acme","visibility":%q},"private":%v,"archived":%v,"default_branch":"main"}`,
				name, name, ownerVis, private, archived)
		}
		switch {
		case p == "/api/v1/orgs/acme/repos":
			_, _ = fmt.Fprintf(w, "[%s,%s,%s,%s]", repo("open-kit", "public", false, false), repo("secret-lab", "public", true, false),
				repo("looks-public", "limited", false, false), repo("old-vault", "public", true, true))
		case strings.HasPrefix(p, "/api/v1/repos/acme/open-kit/contents"):
			dir := strings.TrimPrefix(strings.TrimPrefix(p, "/api/v1/repos/acme/open-kit/contents"), "/")
			if body, ok := good[dir]; ok {
				_, _ = w.Write([]byte(body))
				return
			}
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"GetContentsOrList"}`))
		case strings.HasSuffix(p, "/contents") || strings.HasSuffix(p, "/contents/"):
			if strings.Contains(p, "/secret-lab/") {
				_, _ = w.Write([]byte(`[{"path":"README.md","type":"file"},{"path":".baseliner.yml","type":"file"}]`))
				return
			}
			_, _ = w.Write([]byte(`[]`))
		case strings.Contains(p, "/contents/"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"GetContentsOrList"}`))
		case strings.HasSuffix(p, "/raw/readme.md"):
			_, _ = w.Write([]byte("# Title\n"))
		case strings.HasSuffix(p, "/raw/.baseliner.yml"):
			_, _ = w.Write([]byte("waivers:\n  - check: license_exists\n    reason: ZZQREASON private detail\n"))
		case strings.HasSuffix(p, "/branches"):
			_, _ = w.Write([]byte(`[{"name":"main"}]`))
		case strings.Contains(p, "/branches/"):
			_, _ = fmt.Fprintf(w, `{"name":"main","commit":{"timestamp":%q}}`, recent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

type giteaScan struct {
	code                       int
	stdout, stderr, logs, json string
}

func scanGitea(t *testing.T, srv *httptest.Server, extra string, opts Options) giteaScan {
	t.Helper()
	t.Setenv("GITEA_TOKEN", "test-token")
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)
	dir := t.TempDir()
	cfg := filepath.Join(dir, "baseliner.yaml")
	body := fmt.Sprintf("scope:\n  gitea:\n    type: org\n    name: acme\n    base_url: %s\npolicy:\n  repo_waivers:\n    allow: [license_exists]\n%s", srv.URL, extra)
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	opts.ConfigPath, opts.Format = cfg, "both"
	opts.OutputFile = filepath.Join(dir, "results.json")
	code, stdout, stderr := run(opts)
	b, _ := os.ReadFile(opts.OutputFile)
	return giteaScan{code: code, stdout: stdout, stderr: stderr, logs: logs.String(), json: string(b)}
}

func (s giteaScan) sinks() map[string]string {
	return map[string]string{"stdout": s.stdout, "stderr": s.stderr, "log": s.logs, "results.json": s.json}
}

func TestScanGiteaEndToEnd(t *testing.T) {
	for _, mode := range []string{"", "redact", "exclude"} {
		t.Run("mode="+mode, func(t *testing.T) {
			extra := "privacy:\n  public_context: false\n"
			if mode != "" {
				extra = "privacy:\n  public_context: true\n  private_repos: " + mode + "\n"
			}
			s := scanGitea(t, fakeGitea(t, nil), extra, Options{FailUnder: fptr(0.99)})
			if s.code != 1 || !strings.Contains(s.stdout, "acme/open-kit") {
				t.Fatalf("exit = %d\nstdout:\n%s\nstderr:\n%s", s.code, s.stdout, s.stderr)
			}
			if mode == "" {
				for sink, text := range s.sinks() {
					if sink != "log" && !strings.Contains(strings.ToLower(text), "acme/secret-lab") {
						t.Errorf("with the guard off %s should name acme/secret-lab:\n%s", sink, text)
					}
				}
				if checkStatus(t, s.json, "acme/secret-lab", "license_exists") != "waived" {
					t.Errorf("the private repo's waiver should apply:\n%s", s.json)
				}
				return
			}
			for sink, text := range s.sinks() {
				low := strings.ToLower(text)
				for _, n := range append(giteaPrivateNames, "zzqreason") {
					if strings.Contains(low, n) {
						t.Errorf("%s names %q:\n%s", sink, n, text)
					}
				}
			}
			if mode == "exclude" && !strings.Contains(s.json, `"total_repos": 1`) {
				t.Errorf("exclude mode: only the public repo should be disclosed:\n%s", s.json)
			}
			if mode == "redact" && !strings.Contains(s.stdout, "private/") {
				t.Errorf("redact mode should show masked rows:\n%s", s.stdout)
			}
		})
	}
}

// Every call about a non-public repo fails, with a message that names it; no
// sink may show it, and exclude mode leaves no masked trace.
func TestScanGiteaRedactsEveryAPIFault(t *testing.T) {
	for _, mode := range []string{"redact", "exclude"} {
		for _, status := range []int{http.StatusInternalServerError, http.StatusForbidden, http.StatusNotFound, http.StatusTooManyRequests} {
			t.Run(fmt.Sprintf("%s/%d", mode, status), func(t *testing.T) {
				srv := fakeGitea(t, func(w http.ResponseWriter, _ *http.Request) bool {
					w.WriteHeader(status)
					_, _ = w.Write([]byte(`{"message":"failed for acme/secret-lab and acme/looks-public","errors":["acme/old-vault"]}`))
					return true
				})
				s := scanGitea(t, srv, "privacy:\n  public_context: true\n  private_repos: "+mode+"\n", Options{MinCoverage: fptr(1.0)})
				if s.code == 0 {
					t.Fatalf("exit = 0 although every read of the private repos failed\nstderr:\n%s", s.stderr)
				}
				for sink, text := range s.sinks() {
					low := strings.ToLower(text)
					for _, n := range giteaPrivateNames {
						if strings.Contains(low, n) {
							t.Errorf("%s names %q:\n%s", sink, n, text)
						}
					}
				}
				if mode == "exclude" && strings.Contains(s.logs, privacy.RedactedSlug) {
					t.Errorf("exclude mode left a masked line:\n%s", s.logs)
				}
			})
		}
	}
}

func TestScanGiteaOpenIssuesRefused(t *testing.T) {
	s := scanGitea(t, fakeGitea(t, nil), "", Options{OpenIssues: true, DryRun: true})
	if s.code != 2 || !strings.Contains(s.stderr, "GitHub repos only") {
		t.Errorf("exit %d\n%s", s.code, s.stderr)
	}
}

// In exclude mode a warning about a private repo is dropped even when the
// error names nothing: the warning carries the slug, which the guard knows.
func TestScanGiteaExcludeDropsUnnamedFaults(t *testing.T) {
	srv := fakeGitea(t, func(w http.ResponseWriter, _ *http.Request) bool {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"boom"}`))
		return true
	})
	s := scanGitea(t, srv, "privacy:\n  public_context: true\n  private_repos: exclude\n", Options{})
	if strings.Contains(s.logs, "boom") {
		t.Errorf("exclude mode logged a fault about a private repo:\n%s", s.logs)
	}
	s = scanGitea(t, srv, "privacy:\n  public_context: false\n", Options{})
	if !strings.Contains(s.logs, "boom") {
		t.Errorf("with the guard off the faults should be logged:\n%s", s.logs)
	}
}

// The instance's rate limit makes the scan incomplete: exit 2, with a count.
func TestScanGiteaRateLimit(t *testing.T) {
	srv := fakeGitea(t, func(w http.ResponseWriter, _ *http.Request) bool {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		return true
	})
	s := scanGitea(t, srv, "privacy:\n  public_context: true\n", Options{})
	if s.code != 2 || !strings.Contains(s.stderr, "Gitea refused") {
		t.Errorf("exit = %d\n%s", s.code, s.stderr)
	}
}
