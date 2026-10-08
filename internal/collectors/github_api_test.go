package collectors

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

// A listing or README read that fails with anything but 404 is unreadable
// evidence, not absence: the collector records which, so the checks that
// depend on it report unknown rather than failing as if the files were
// missing. What was read is kept, and 404 still means absent.
func TestGitHubAPICollectRecordsUnreadEvidence(t *testing.T) {
	for _, c := range []struct {
		path         string
		status       int
		wantUnread   []string
		readmeUnread bool
	}{
		{"/repos/o/r/contents/", http.StatusInternalServerError, []string{""}, false},
		{"/repos/o/r/contents/.github/workflows", http.StatusInternalServerError, []string{".github/workflows"}, false},
		{"/repos/o/r/contents/docs", http.StatusForbidden, []string{"docs"}, false},
		{"/repos/o/r/readme", http.StatusBadGateway, nil, true},
	} {
		t.Run(c.path, func(t *testing.T) {
			h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case c.path: // first, so it wins over the healthy cases below
					http.Error(w, `{"message":"boom"}`, c.status)
				case "/repos/o/r/contents/":
					_, _ = w.Write([]byte(`[{"type":"file","name":"README.md","path":"README.md"}]`))
				case "/repos/o/r/readme":
					_, _ = w.Write([]byte(`{"encoding":"base64","content":"IyBUaXRsZQ=="}`))
				case "/repos/o/r/branches":
					_, _ = w.Write([]byte(`[{"name":"main"}]`))
				default:
					http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
				}
			})
			repo := &github.Repository{Owner: &github.User{Login: github.Ptr("o")}, Name: github.Ptr("r"), DefaultBranch: github.Ptr("main")}
			col := GitHubAPI{Client: fakeGitHubClient(t, h), StaleThresholdDays: 90}
			got := col.Collect(context.Background(), source.Repo{Type: "github", Slug: "o/r", GitHubRepo: repo})
			if fmt.Sprintf("%q", got.FS.UnreadDirs) != fmt.Sprintf("%q", c.wantUnread) || got.FS.ReadmeUnread != c.readmeUnread {
				t.Errorf("unread dirs %q, readme unread %v; want %q, %v", got.FS.UnreadDirs, got.FS.ReadmeUnread, c.wantUnread, c.readmeUnread)
			}
			if c.path != "/repos/o/r/contents/" && !got.FS.KeyFiles["README"] {
				t.Error("the root listing was read, so README should still be detected")
			}
		})
	}
}

// A README the API returns but that cannot be decoded is unreadable too.
func TestGitHubAPICollectUndecodableReadmeIsUnread(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/readme":
			_, _ = w.Write([]byte(`{"encoding":"base64","content":"not base64 !!"}`))
		case "/repos/o/r/contents/":
			_, _ = w.Write([]byte(`[{"type":"file","name":"README.md","path":"README.md"}]`))
		default:
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		}
	})
	repo := &github.Repository{Owner: &github.User{Login: github.Ptr("o")}, Name: github.Ptr("r")}
	col := GitHubAPI{Client: fakeGitHubClient(t, h), StaleThresholdDays: 90}
	if got := col.Collect(context.Background(), source.Repo{Type: "github", Slug: "o/r", GitHubRepo: repo}); !got.FS.ReadmeUnread {
		t.Errorf("ReadmeUnread = false after an undecodable README: %+v", got.FS)
	}
}

// For a README over 1 MB the contents API sends encoding "none" and no
// content. That is not unreadable: the README is fetched raw instead.
func TestGitHubAPICollectLargeReadmeFetchedRaw(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/readme":
			if strings.Contains(r.Header.Get("Accept"), "raw") {
				_, _ = w.Write([]byte("# Big\n\nbody"))
				return
			}
			_, _ = w.Write([]byte(`{"name":"README.md","path":"README.md","encoding":"none","content":"","size":2000000}`))
		case "/repos/o/r/contents/":
			_, _ = w.Write([]byte(`[{"type":"file","name":"README.md","path":"README.md"}]`))
		default:
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		}
	})
	repo := &github.Repository{Owner: &github.User{Login: github.Ptr("o")}, Name: github.Ptr("r")}
	col := GitHubAPI{Client: fakeGitHubClient(t, h), StaleThresholdDays: 90}
	got := col.Collect(context.Background(), source.Repo{Type: "github", Slug: "o/r", GitHubRepo: repo})
	if got.FS.ReadmeUnread || got.FS.ReadmeContent == nil || *got.FS.ReadmeContent != "# Big\n\nbody" {
		t.Errorf("readme unread=%v content=%v, want the raw README", got.FS.ReadmeUnread, got.FS.ReadmeContent)
	}
}

// readmeCase runs Collect with the given /readme handler and a root listing
// holding README.md.
func readmeCase(t *testing.T, readme http.HandlerFunc) *GitHubAPI {
	t.Helper()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/readme":
			readme(w, r)
		case "/repos/o/r/contents/":
			_, _ = w.Write([]byte(`[{"type":"file","name":"README.md","path":"README.md"}]`))
		default:
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		}
	})
	return &GitHubAPI{Client: fakeGitHubClient(t, h), StaleThresholdDays: 90}
}

// A 404 on the README means there is none: that is evidence, not an unread
// README, so the README-content checks still fail rather than going unknown.
func TestGitHubAPICollectReadme404IsAbsent(t *testing.T) {
	col := readmeCase(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})
	repo := &github.Repository{Owner: &github.User{Login: github.Ptr("o")}, Name: github.Ptr("r")}
	got := col.Collect(context.Background(), source.Repo{Type: "github", Slug: "o/r", GitHubRepo: repo})
	if got.FS.ReadmeUnread || got.FS.ReadmeContent != nil {
		t.Errorf("unread=%v content=%v, want an absent README", got.FS.ReadmeUnread, got.FS.ReadmeContent)
	}
}

// A large README whose raw fetch fails is unread, not empty.
func TestGitHubAPICollectRawReadmeFailureIsUnread(t *testing.T) {
	col := readmeCase(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Accept"), "raw") {
			http.Error(w, `{"message":"boom"}`, http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"name":"README.md","path":"README.md","encoding":"none","content":"","size":2000000}`))
	})
	repo := &github.Repository{Owner: &github.User{Login: github.Ptr("o")}, Name: github.Ptr("r")}
	got := col.Collect(context.Background(), source.Repo{Type: "github", Slug: "o/r", GitHubRepo: repo})
	if !got.FS.ReadmeUnread || got.FS.ReadmeContent != nil {
		t.Errorf("unread=%v content=%v, want an unread README", got.FS.ReadmeUnread, got.FS.ReadmeContent)
	}
}

// The raw fetch keeps maxReadmeBytes and stops; reaching the limit is not a
// failure.
func TestGitHubAPICollectRawReadmeTruncates(t *testing.T) {
	col := readmeCase(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Accept"), "raw") {
			_, _ = w.Write([]byte("# Big\n" + strings.Repeat("x", 3*maxReadmeBytes)))
			return
		}
		_, _ = w.Write([]byte(`{"name":"README.md","path":"README.md","encoding":"none","content":"","size":2000000}`))
	})
	repo := &github.Repository{Owner: &github.User{Login: github.Ptr("o")}, Name: github.Ptr("r")}
	got := col.Collect(context.Background(), source.Repo{Type: "github", Slug: "o/r", GitHubRepo: repo})
	if got.FS.ReadmeUnread || got.FS.ReadmeContent == nil || len(*got.FS.ReadmeContent) != maxReadmeBytes {
		t.Errorf("unread=%v len=%v, want the first %d bytes", got.FS.ReadmeUnread, got.FS.ReadmeContent != nil, maxReadmeBytes)
	}
}

// Visibility reports what GitHub says, keeping internal distinct from private,
// and lets private: true win over a missing or contradicting "public".
func TestVisibility(t *testing.T) {
	for _, c := range []struct {
		repo *github.Repository
		want string
	}{
		{&github.Repository{Visibility: github.Ptr("public")}, "public"},
		{&github.Repository{Visibility: github.Ptr("Private")}, "private"},
		{&github.Repository{Visibility: github.Ptr("internal"), Private: github.Ptr(true)}, "internal"},
		{&github.Repository{Visibility: github.Ptr("public"), Private: github.Ptr(true)}, "private"},
		{&github.Repository{Private: github.Ptr(true)}, "private"},
		{&github.Repository{}, "public"},
	} {
		if got := Visibility(c.repo); got != c.want {
			t.Errorf("Visibility(visibility=%q private=%v) = %q, want %q", c.repo.GetVisibility(), c.repo.GetPrivate(), got, c.want)
		}
	}
}
