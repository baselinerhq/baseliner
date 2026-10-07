package collectors

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-github/v68/github"
)

// ciMux serves a repo with the given workflow files and Actions workflows
// handler; everything else 404s.
func ciMux(files []string, workflows http.HandlerFunc) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/o/r/contents/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/o/r/contents/.github/workflows" && len(files) > 0 {
			body := "["
			for i, f := range files {
				if i > 0 {
					body += ","
				}
				body += fmt.Sprintf(`{"type":"file","path":%q}`, f)
			}
			_, _ = w.Write([]byte(body + "]"))
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("GET /repos/o/r/actions/workflows", workflows)
	mux.HandleFunc("GET /", http.NotFound)
	return mux
}

func collectInactive(t *testing.T, mux *http.ServeMux) map[string]string {
	t.Helper()
	repo := &github.Repository{Owner: &github.User{Login: github.Ptr("o")}, Name: github.Ptr("r"),
		DefaultBranch: github.Ptr("main")}
	return NewGitHubAPI(fakeGitHubClient(t, mux)).Collect(context.Background(), ghSource(repo)).FS.InactiveCIFiles
}

const ciYML = ".github/workflows/ci.yml"

func TestCollectReportsDisabledWorkflows(t *testing.T) {
	inactive := collectInactive(t, ciMux([]string{ciYML}, respond(200, `{"total_count":2,"workflows":[`+
		`{"path":".github/workflows/ci.yml","state":"disabled_inactivity"},`+
		`{"path":"dynamic/github-code-scanning/codeql","state":"active"}]}`)))
	if inactive[ciYML] != "disabled_inactivity" || len(inactive) != 1 {
		t.Errorf("InactiveCIFiles = %v, want only ci.yml as disabled_inactivity", inactive)
	}
}

func TestCollectActiveWorkflowsAreNotInactive(t *testing.T) {
	inactive := collectInactive(t, ciMux([]string{ciYML}, respond(200,
		`{"total_count":1,"workflows":[{"path":".github/workflows/ci.yml","state":"active"}]}`)))
	if len(inactive) != 0 {
		t.Errorf("InactiveCIFiles = %v, want none", inactive)
	}
}

// A fork whose workflows were never enabled has the files, but GitHub lists
// none of them. A file the listing does not show as active is not working CI.
func TestCollectUnlistedWorkflowIsInactive(t *testing.T) {
	inactive := collectInactive(t, ciMux([]string{ciYML}, respond(200, `{"total_count":0,"workflows":[]}`)))
	if _, ok := inactive[ciYML]; !ok {
		t.Errorf("InactiveCIFiles = %v, want ci.yml (not listed by GitHub Actions)", inactive)
	}
}

// The listing pages. A workflow listed as active on page 2 must not be
// reported as unlisted.
func TestCollectWorkflowListingIsPaginated(t *testing.T) {
	inactive := collectInactive(t, ciMux([]string{ciYML}, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`{"total_count":2,"workflows":[{"path":".github/workflows/ci.yml","state":"active"}]}`))
			return
		}
		w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/o/r/actions/workflows?page=2>; rel="next"`, r.Host))
		_, _ = w.Write([]byte(`{"total_count":2,"workflows":[{"path":".github/workflows/other.yml","state":"active"}]}`))
	}))
	if len(inactive) != 0 {
		t.Errorf("InactiveCIFiles = %v, want none (ci.yml is active on page 2)", inactive)
	}
}

// A listing that fails part-way is unknown, never partial: under "unlisted is
// inactive", files on the unread pages would otherwise fail falsely.
func TestCollectPartialListingIsUnknown(t *testing.T) {
	inactive := collectInactive(t, ciMux([]string{ciYML}, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/o/r/actions/workflows?page=2>; rel="next"`, r.Host))
		_, _ = w.Write([]byte(`{"total_count":2,"workflows":[{"path":".github/workflows/other.yml","state":"active"}]}`))
	}))
	if inactive != nil {
		t.Errorf("InactiveCIFiles = %v, want nil (unknown) after a page-2 failure", inactive)
	}
}

// No Actions read access: workflow state is unknown, so ci_present falls back
// to file presence rather than losing coverage.
func TestCollectWorkflowStateUnreadableFallsBack(t *testing.T) {
	inactive := collectInactive(t, ciMux([]string{ciYML}, respond(403, `{"message":"Resource not accessible by personal access token"}`)))
	if inactive != nil {
		t.Errorf("InactiveCIFiles = %v, want nil when the Actions API is unreadable", inactive)
	}
}

// A repo with no workflow files costs no Actions call.
func TestCollectSkipsActionsCallWithoutWorkflowFiles(t *testing.T) {
	calls := 0
	collectInactive(t, ciMux(nil, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"total_count":0,"workflows":[]}`))
	}))
	if calls != 0 {
		t.Errorf("Actions workflows called %d time(s) for a repo with no workflow files", calls)
	}
}

// A listing longer than maxWorkflowPages is not read in full, so it is unknown.
func TestCollectWorkflowPageCapIsUnknown(t *testing.T) {
	calls := 0
	inactive := collectInactive(t, ciMux([]string{ciYML}, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/o/r/actions/workflows?page=%d>; rel="next"`, r.Host, calls+1))
		_, _ = w.Write([]byte(`{"total_count":5000,"workflows":[{"path":".github/workflows/other.yml","state":"active"}]}`))
	}))
	if inactive != nil || calls != maxWorkflowPages {
		t.Errorf("InactiveCIFiles = %v after %d call(s), want nil after %d", inactive, calls, maxWorkflowPages)
	}
}

// The fallback reads as a pass for disabled workflows, so it is logged at
// warn level, once per run however many repos hit it.
func TestFallbackWarnsOncePerRun(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	c := NewGitHubAPI(fakeGitHubClient(t, ciMux([]string{ciYML}, respond(403, `{"message":"forbidden"}`))))
	repo := &github.Repository{Owner: &github.User{Login: github.Ptr("o")}, Name: github.Ptr("r"),
		DefaultBranch: github.Ptr("main")}
	for range 3 {
		c.Collect(context.Background(), ghSource(repo))
	}
	if n := strings.Count(buf.String(), "falls back to file presence"); n != 1 {
		t.Errorf("fallback warning logged %d time(s), want 1:\n%s", n, buf.String())
	}
}
