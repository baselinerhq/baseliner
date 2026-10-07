package collectors

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"testing"

	"github.com/google/go-github/v68/github"
)

// ciMux serves a repo with the given files (listed under their directories)
// and Actions workflows handler; everything else 404s.
func ciMux(files []string, workflows http.HandlerFunc) *http.ServeMux {
	dirs := map[string][]string{}
	for _, f := range files {
		dirs[path.Dir(f)] = append(dirs[path.Dir(f)], f)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/o/r/contents/", func(w http.ResponseWriter, r *http.Request) {
		entries, ok := dirs[strings.TrimPrefix(r.URL.Path, "/repos/o/r/contents/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		body := "["
		for i, f := range entries {
			if i > 0 {
				body += ","
			}
			body += fmt.Sprintf(`{"type":"file","path":%q}`, f)
		}
		_, _ = w.Write([]byte(body + "]"))
	})
	mux.HandleFunc("GET /repos/o/r/actions/workflows", workflows)
	mux.HandleFunc("GET /", http.NotFound)
	return mux
}

func ciRepo(fork bool) *github.Repository {
	return &github.Repository{Owner: &github.User{Login: github.Ptr("o")}, Name: github.Ptr("r"),
		DefaultBranch: github.Ptr("main"), Fork: github.Ptr(fork)}
}

func collectInactiveRepo(t *testing.T, mux *http.ServeMux, fork bool) map[string]string {
	t.Helper()
	return NewGitHubAPI(fakeGitHubClient(t, mux)).Collect(context.Background(), ghSource(ciRepo(fork))).FS.InactiveCIFiles
}

func collectInactive(t *testing.T, mux *http.ServeMux) map[string]string {
	t.Helper()
	return collectInactiveRepo(t, mux, false)
}

// captureWarnings routes slog's default logger to a buffer at warn level for
// the rest of the test.
func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

const ciYML = ".github/workflows/ci.yml"

// nextPage links a response to the page after the given one.
func nextPage(w http.ResponseWriter, r *http.Request, page int) {
	w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/o/r/actions/workflows?page=%d>; rel="next"`, r.Host, page+1))
}

func TestCollectReportsDisabledWorkflows(t *testing.T) {
	inactive := collectInactive(t, ciMux([]string{ciYML}, respond(200, `{"total_count":2,"workflows":[`+
		`{"path":".github/workflows/ci.yml","state":"disabled_inactivity"},`+
		`{"path":"dynamic/github-code-scanning/codeql","state":"active"}]}`)))
	if inactive[ciYML] != "disabled_inactivity" || len(inactive) != 1 {
		t.Errorf("InactiveCIFiles = %v, want only ci.yml as disabled_inactivity", inactive)
	}
}

func TestCollectActiveWorkflowsAreNotInactive(t *testing.T) {
	inactive := collectInactiveRepo(t, ciMux([]string{ciYML}, respond(200,
		`{"total_count":1,"workflows":[{"path":".github/workflows/ci.yml","state":"active"}]}`)), true)
	if inactive == nil || len(inactive) != 0 {
		t.Errorf("InactiveCIFiles = %v, want known and empty", inactive)
	}
}

// A fork whose workflows were never enabled has the files, but GitHub lists
// none of them, so on a fork an unlisted file is not working CI.
func TestCollectUnlistedForkWorkflowIsInactive(t *testing.T) {
	inactive := collectInactiveRepo(t, ciMux([]string{ciYML}, respond(200, `{"total_count":0,"workflows":[]}`)), true)
	if inactive[ciYML] != notListed {
		t.Errorf("InactiveCIFiles = %v, want ci.yml (%s)", inactive, notListed)
	}
}

// On a non-fork GitHub lists a workflow only once an event or a push to the
// file has reached it, so an unlisted file may be valid CI that has not run.
func TestCollectUnlistedWorkflowOnNonForkCounts(t *testing.T) {
	inactive := collectInactiveRepo(t, ciMux([]string{ciYML}, respond(200, `{"total_count":0,"workflows":[]}`)), false)
	if inactive == nil || len(inactive) != 0 {
		t.Errorf("InactiveCIFiles = %v, want known and empty on a non-fork", inactive)
	}
}

// The listing pages. A state that only page 2 reports must be read.
func TestCollectWorkflowListingIsPaginated(t *testing.T) {
	inactive := collectInactive(t, ciMux([]string{ciYML}, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`{"total_count":2,"workflows":[{"path":".github/workflows/ci.yml","state":"disabled_manually"}]}`))
			return
		}
		nextPage(w, r, 1)
		_, _ = w.Write([]byte(`{"total_count":2,"workflows":[{"path":".github/workflows/other.yml","state":"active"}]}`))
	}))
	if inactive[ciYML] != "disabled_manually" || len(inactive) != 1 {
		t.Errorf("InactiveCIFiles = %v, want ci.yml as disabled_manually from page 2", inactive)
	}
}

// A listing that fails part-way is unknown, never partial: on a fork, files on
// the unread pages would otherwise fail falsely as unlisted.
func TestCollectPartialListingIsUnknown(t *testing.T) {
	inactive := collectInactiveRepo(t, ciMux([]string{ciYML}, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
			return
		}
		nextPage(w, r, 1)
		_, _ = w.Write([]byte(`{"total_count":2,"workflows":[{"path":".github/workflows/other.yml","state":"active"}]}`))
	}), true)
	if inactive != nil {
		t.Errorf("InactiveCIFiles = %v, want nil (unknown) after a page-2 failure", inactive)
	}
}

// A listing longer than the page cap is not read in full, so it is unknown,
// and the fallback is announced.
func TestCollectWorkflowPageCapIsUnknown(t *testing.T) {
	warnings := captureWarnings(t)
	calls := 0
	inactive := collectInactiveRepo(t, ciMux([]string{ciYML}, func(w http.ResponseWriter, r *http.Request) {
		calls++
		nextPage(w, r, calls)
		_, _ = w.Write([]byte(`{"total_count":5000,"workflows":[{"path":".github/workflows/other.yml","state":"active"}]}`))
	}), true)
	if inactive != nil || calls != 10 {
		t.Errorf("InactiveCIFiles = %v after %d call(s), want nil after 10", inactive, calls)
	}
	if !strings.Contains(warnings.String(), "falls back to file presence") {
		t.Errorf("no fallback warning at the page cap:\n%s", warnings.String())
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

// A repo with no workflow files costs no Actions call, and CI that is not
// GitHub Actions is never marked unlisted, even on a fork.
func TestCollectSkipsActionsCallWithoutWorkflowFiles(t *testing.T) {
	for _, files := range [][]string{nil, {".circleci/config.yml"}} {
		calls := 0
		inactive := collectInactiveRepo(t, ciMux(files, func(w http.ResponseWriter, _ *http.Request) {
			calls++
			_, _ = w.Write([]byte(`{"total_count":0,"workflows":[]}`))
		}), true)
		if calls != 0 || inactive != nil {
			t.Errorf("files %v: %d Actions call(s), InactiveCIFiles = %v; want none and nil", files, calls, inactive)
		}
	}
}

// The fallback reads as a pass for disabled workflows, so it is logged at
// warn level, once per run however many collections hit it.
func TestFallbackWarnsOncePerRun(t *testing.T) {
	warnings := captureWarnings(t)
	c := NewGitHubAPI(fakeGitHubClient(t, ciMux([]string{ciYML}, respond(403, `{"message":"forbidden"}`))))
	for range 3 {
		c.Collect(context.Background(), ghSource(ciRepo(false)))
	}
	if n := strings.Count(warnings.String(), "falls back to file presence"); n != 1 {
		t.Errorf("fallback warning logged %d time(s), want 1:\n%s", n, warnings.String())
	}
}
