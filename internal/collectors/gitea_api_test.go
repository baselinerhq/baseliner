package collectors

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/baselinerhq/baseliner/internal/gitea"
	"github.com/baselinerhq/baseliner/internal/models"
	"github.com/baselinerhq/baseliner/internal/source"
)

// gtFake serves repo o/r: contents by directory, raw files by path, in the
// shapes Forgejo 16 returns. A missing key is Gitea's 404 for it.
type gtFake struct {
	dirs  map[string]any // dir -> JSON body or an int status
	files map[string]any // path -> body or an int status
	hits  atomic.Int32
}

func (f *gtFake) handler(w http.ResponseWriter, r *http.Request) {
	f.hits.Add(1)
	p := strings.TrimPrefix(r.URL.Path, "/api/v1/repos/o/r")
	write := func(v any, notFound string) {
		switch b := v.(type) {
		case nil:
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprintf(w, `{"message":%q}`, notFound)
		case int:
			w.WriteHeader(b)
			_, _ = w.Write([]byte(`{"message":"boom"}`))
		case string:
			_, _ = w.Write([]byte(b))
		}
	}
	switch {
	case strings.HasPrefix(p, "/contents/") || p == "/contents":
		write(f.dirs[strings.TrimPrefix(strings.TrimPrefix(p, "/contents"), "/")], "GetContentsOrList")
	case strings.HasPrefix(p, "/raw/"):
		write(f.files[strings.TrimPrefix(p, "/raw/")], "The target couldn't be found.")
	case p == "/branches":
		if pg := r.URL.Query().Get("page"); pg != "" && pg != "1" {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_, _ = w.Write([]byte(`[{"name":"main"},{"name":"dev"}]`))
	case strings.HasPrefix(p, "/branches/"):
		_, _ = w.Write([]byte(`{"name":"main","commit":{"timestamp":"2026-10-01T00:00:00Z"}}`))
	default:
		http.NotFound(w, r)
	}
}

func gtCollect(t *testing.T, f *gtFake, r gitea.Repo, observe func(error)) *models.NormalizedRepository {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(srv.Close)
	client, err := gitea.New(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	c := NewGiteaAPI(client)
	c.Now = func() time.Time { return time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC) }
	c.Observe = observe
	return c.Collect(context.Background(), source.Repo{Type: "gitea", Slug: "acme/r", Visibility: "private", ForgeRepo: &r})
}

func giteaRepo() gitea.Repo {
	return gitea.Repo{Name: "r", Owner: gitea.Owner{Login: "o"}, DefaultBranch: "main"}
}

func TestGiteaCollect(t *testing.T) {
	f := &gtFake{
		dirs: map[string]any{
			"": `[{"path":"README.md","type":"file"},{"path":"LICENSE","type":"file"},{"path":"link","type":"symlink"},
				{"path":"vendor","type":"submodule"},{"path":"docs","type":"dir"}]`,
			".forgejo/workflows": `[{"path":".forgejo/workflows/ci.yml","type":"file"}]`,
			".gitea":             `[{"path":".gitea/CODEOWNERS","type":"file"}]`,
			"docs":               `{"name":"docs","path":"docs","type":"file"}`, // a file, not a directory
		},
		files: map[string]any{"README.md": "# Title\n" + strings.Repeat("x", 5000)},
	}
	r := gtCollect(t, f, giteaRepo(), nil)
	if strings.Join(r.FS.Files, ",") != ".forgejo/workflows/ci.yml,.gitea/CODEOWNERS,LICENSE,README.md" {
		t.Errorf("files = %v: want files only, no symlink or submodule", r.FS.Files)
	}
	if !r.FS.KeyFiles["CODEOWNERS"] || strings.Join(r.FS.CIFiles, ",") != ".forgejo/workflows/ci.yml" || len(r.FS.UnreadDirs) != 0 {
		t.Errorf("key files %v, CI %v, unread %v", r.FS.KeyFiles, r.FS.CIFiles, r.FS.UnreadDirs)
	}
	if r.FS.ReadmeContent == nil || len(*r.FS.ReadmeContent) != maxReadmeBytes || r.SourceType != models.SourceGitea || r.Visibility != "private" {
		t.Errorf("readme / source: %+v", r)
	}
	if *r.Git.DaysSinceCommit != 7 || r.Git.IsStale || strings.Join(r.Git.Branches, ",") != "main,dev" {
		t.Errorf("git = %+v", r.Git)
	}
}

// Gitea answers a missing ref and a missing directory with the same 404, so a
// directory's 404 is absence only below a root that was read. A listed file
// that cannot be read is unread, a 404 included.
func TestGiteaCollectUnreadEvidence(t *testing.T) {
	var observed atomic.Int32
	f := &gtFake{
		dirs:  map[string]any{"": `[{"path":"README.md","type":"file"}]`, ".github": http.StatusInternalServerError, "docs": http.StatusForbidden},
		files: map[string]any{},
	}
	r := gtCollect(t, f, giteaRepo(), func(error) { observed.Add(1) })
	if strings.Join(r.FS.UnreadDirs, ",") != ".github,docs" || !r.FS.ReadmeUnread {
		t.Errorf("unread %v, readme unread %v", r.FS.UnreadDirs, r.FS.ReadmeUnread)
	}
	if observed.Load() != 3 {
		t.Errorf("observed %d errors, want 3", observed.Load())
	}

	// A root that cannot be read leaves every directory unread, 404s included.
	r = gtCollect(t, &gtFake{dirs: map[string]any{"": http.StatusNotFound}}, giteaRepo(), nil)
	if len(r.FS.UnreadDirs) != len(giteaEvidenceDirs) || !r.FS.ReadmeUnread {
		t.Errorf("unread %v, README unread %v; want every directory and the README", r.FS.UnreadDirs, r.FS.ReadmeUnread)
	}
}

// An empty repo is read as empty without requests; a repo with no default
// branch that is not empty could not be read.
func TestGiteaCollectEmptyAndUnreadable(t *testing.T) {
	f := &gtFake{}
	r := gtCollect(t, f, gitea.Repo{Name: "r", Owner: gitea.Owner{Login: "o"}, Empty: true, DefaultBranch: "main"}, nil)
	if f.hits.Load() != 0 || len(r.FS.UnreadDirs) != 0 || r.FS.ReadmeUnread {
		t.Errorf("empty: %d requests, unread %v", f.hits.Load(), r.FS.UnreadDirs)
	}
	f = &gtFake{}
	r = gtCollect(t, f, gitea.Repo{Name: "r", Owner: gitea.Owner{Login: "o"}}, nil)
	if f.hits.Load() != 0 || len(r.FS.UnreadDirs) != len(giteaEvidenceDirs) || !r.FS.ReadmeUnread {
		t.Errorf("no default branch: %d requests, unread %v", f.hits.Load(), r.FS.UnreadDirs)
	}
}

func TestGiteaCollectWaivers(t *testing.T) {
	root := `[{"path":".baseliner.yml","type":"file"}]`
	r := gtCollect(t, &gtFake{dirs: map[string]any{"": root}, files: map[string]any{".baseliner.yml": "waivers:\n  - check: ci_present\n    reason: docs only\n"}}, giteaRepo(), nil)
	if len(r.Waivers) != 1 {
		t.Errorf("waivers = %+v", r.Waivers)
	}
	r = gtCollect(t, &gtFake{dirs: map[string]any{"": `[{"path":".baseliner.yml","type":"symlink"}]`}, files: map[string]any{".baseliner.yml": "waivers:\n  - check: ci_present\n    reason: x\n"}}, giteaRepo(), nil)
	if r.Waivers != nil {
		t.Errorf("a symlinked waiver file was read: %+v", r.Waivers)
	}
}
