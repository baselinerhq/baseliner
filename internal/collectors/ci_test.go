package collectors

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/go-github/v68/github"
)

// ciMux serves a repo with one workflow file and the given Actions workflows
// response; everything else 404s.
func ciMux(workflows http.HandlerFunc) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/o/r/contents/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/o/r/contents/.github/workflows" {
			_, _ = w.Write([]byte(`[{"type":"file","name":"ci.yml","path":".github/workflows/ci.yml"}]`))
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("GET /repos/o/r/actions/workflows", workflows)
	mux.HandleFunc("GET /", http.NotFound)
	return mux
}

func collectFS(t *testing.T, mux *http.ServeMux) map[string]string {
	t.Helper()
	repo := &github.Repository{Owner: &github.User{Login: github.Ptr("o")}, Name: github.Ptr("r"),
		DefaultBranch: github.Ptr("main")}
	got := NewGitHubAPI(fakeGitHubClient(t, mux)).Collect(context.Background(), ghSource(repo))
	if len(got.FS.CIFiles) != 1 {
		t.Fatalf("CIFiles = %v, want the one workflow file", got.FS.CIFiles)
	}
	return got.FS.DisabledCIFiles
}

func TestCollectReportsDisabledWorkflows(t *testing.T) {
	disabled := collectFS(t, ciMux(respond(200, `{"total_count":2,"workflows":[`+
		`{"path":".github/workflows/ci.yml","state":"disabled_inactivity"},`+
		`{"path":"dynamic/github-code-scanning/codeql","state":"active"}]}`)))
	if disabled == nil || disabled[".github/workflows/ci.yml"] != "disabled_inactivity" || len(disabled) != 1 {
		t.Errorf("DisabledCIFiles = %v, want only ci.yml as disabled_inactivity", disabled)
	}
}

func TestCollectActiveWorkflowsAreNotDisabled(t *testing.T) {
	disabled := collectFS(t, ciMux(respond(200,
		`{"total_count":1,"workflows":[{"path":".github/workflows/ci.yml","state":"active"}]}`)))
	if disabled == nil || len(disabled) != 0 {
		t.Errorf("DisabledCIFiles = %v, want an empty, non-nil map (state known, none disabled)", disabled)
	}
}

// No Actions read access: workflow state is unknown, so ci_present falls back
// to file presence rather than losing coverage.
func TestCollectWorkflowStateUnreadableFallsBack(t *testing.T) {
	disabled := collectFS(t, ciMux(respond(403, `{"message":"Resource not accessible by personal access token"}`)))
	if disabled != nil {
		t.Errorf("DisabledCIFiles = %v, want nil when the Actions API is unreadable", disabled)
	}
}
