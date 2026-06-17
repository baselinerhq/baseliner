package collectors

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/go-github/v68/github"

	"github.com/baselinerhq/baseliner/internal/source"
)

func fakeGitHubClient(t *testing.T, h http.Handler) *github.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := github.NewClient(nil)
	u, _ := url.Parse(srv.URL + "/")
	c.BaseURL = u
	return c
}

func TestGitHubAPICollect(t *testing.T) {
	mux := http.NewServeMux()
	// One handler for all contents subpaths; dispatch on the request path.
	mux.HandleFunc("GET /repos/o/r/contents/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/contents/":
			_, _ = w.Write([]byte(`[
				{"type":"file","name":"README.md","path":"README.md"},
				{"type":"file","name":"LICENSE","path":"LICENSE"},
				{"type":"dir","name":"internal","path":"internal"}
			]`))
		case "/repos/o/r/contents/.github":
			_, _ = w.Write([]byte(`[
				{"type":"file","name":"CODEOWNERS","path":".github/CODEOWNERS"},
				{"type":"file","name":"dependabot.yml","path":".github/dependabot.yml"}
			]`))
		case "/repos/o/r/contents/.github/workflows":
			_, _ = w.Write([]byte(`[{"type":"file","name":"ci.yml","path":".github/workflows/ci.yml"}]`))
		default: // .circleci and anything else: absent
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		}
	})
	mux.HandleFunc("GET /repos/o/r/readme", func(w http.ResponseWriter, _ *http.Request) {
		// base64 of "# Title"
		_, _ = w.Write([]byte(`{"name":"README.md","path":"README.md","encoding":"base64","content":"IyBUaXRsZQ=="}`))
	})
	mux.HandleFunc("GET /repos/o/r/branches", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"name":"main"},{"name":"dev"}]`))
	})

	now := time.Date(2026, 6, 17, 4, 0, 0, 0, time.UTC)
	pushed := now.AddDate(0, 0, -100) // 100 days stale (>90)
	repo := &github.Repository{
		Owner:         &github.User{Login: github.Ptr("o")},
		Name:          github.Ptr("r"),
		DefaultBranch: github.Ptr("main"),
		PushedAt:      &github.Timestamp{Time: pushed},
	}
	c := GitHubAPI{
		Client:             fakeGitHubClient(t, mux),
		StaleThresholdDays: 90,
		Now:                func() time.Time { return now },
	}

	got := c.Collect(context.Background(), source.Repo{Type: "github", Slug: "o/r", GitHubRepo: repo})

	for _, k := range []string{"README", "LICENSE", "CODEOWNERS"} {
		if !got.FS.KeyFiles[k] {
			t.Errorf("key file %s not detected", k)
		}
	}
	if len(got.FS.CIFiles) != 1 || got.FS.CIFiles[0] != ".github/workflows/ci.yml" {
		t.Errorf("CI files = %v, want [.github/workflows/ci.yml]", got.FS.CIFiles)
	}
	if len(got.FS.DepUpdateFiles) != 1 || got.FS.DepUpdateFiles[0] != ".github/dependabot.yml" {
		t.Errorf("dep-update files = %v", got.FS.DepUpdateFiles)
	}
	if got.FS.ReadmeContent == nil || *got.FS.ReadmeContent != "# Title" {
		t.Errorf("readme content = %v, want '# Title'", got.FS.ReadmeContent)
	}
	if got.Git.DefaultBranch == nil || *got.Git.DefaultBranch != "main" {
		t.Errorf("default branch = %v", got.Git.DefaultBranch)
	}
	if len(got.Git.Branches) != 2 {
		t.Errorf("branches = %v, want 2", got.Git.Branches)
	}
	if !got.Git.IsStale || got.Git.DaysSinceCommit == nil || *got.Git.DaysSinceCommit != 100 {
		t.Errorf("staleness: isStale=%v days=%v, want true/100", got.Git.IsStale, got.Git.DaysSinceCommit)
	}
}

func TestGitHubAPICollectNilRepo(t *testing.T) {
	// A github source without a *github.Repository degrades to an empty result.
	c := NewGitHubAPI(github.NewClient(nil))
	got := c.Collect(context.Background(), source.Repo{Type: "github", Slug: "o/r"})
	if got == nil || len(got.FS.Files) != 0 || got.Git.DefaultBranch != nil {
		t.Errorf("expected empty result for nil repo, got %+v", got)
	}
}
