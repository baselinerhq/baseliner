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

	"github.com/baselinerhq/baseliner/internal/gitlab"
	"github.com/baselinerhq/baseliner/internal/models"
	"github.com/baselinerhq/baseliner/internal/source"
	"github.com/baselinerhq/baseliner/internal/waivers"
)

// glFake serves project 9's trees and files from maps keyed by directory and
// by escaped file path; a missing key is a 404 for it, a status is returned
// as given.
type glFake struct {
	trees    map[string]any // dir -> JSON body, an int status, or endless for a listing that never ends
	files    map[string]any // escaped path -> body, or an int status
	branches any
	treeHits atomic.Int32
}

func (f *glFake) handler(w http.ResponseWriter, r *http.Request) {
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
		case endless:
			w.Header().Set("X-Next-Page", "2")
			_, _ = w.Write([]byte(`[{"path":"x","type":"blob","mode":"100644"}]`))
		}
	}
	p := r.URL.EscapedPath()
	switch {
	case p == "/api/v4/projects/9/repository/tree":
		f.treeHits.Add(1)
		if r.URL.Query().Get("ref") != "main" {
			write(nil, "404 invalid revision or path Not Found")
			return
		}
		write(f.trees[r.URL.Query().Get("path")], "404 invalid revision or path Not Found")
	case strings.HasPrefix(p, "/api/v4/projects/9/repository/files/"):
		name := strings.TrimSuffix(strings.TrimPrefix(p, "/api/v4/projects/9/repository/files/"), "/raw")
		write(f.files[name], "404 File Not Found")
	case p == "/api/v4/projects/9/repository/branches":
		if f.branches == nil {
			write(`[{"name":"main"},{"name":"dev"}]`, "")
			return
		}
		write(f.branches, "")
	default:
		http.NotFound(w, r)
	}
}

// endless is a tree listing whose every page says there is another.
type endless struct{}

func glCollect(t *testing.T, f *glFake, p gitlab.Project, observe func(error)) *models.NormalizedRepository {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(srv.Close)
	client, err := gitlab.New(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	c := NewGitLabAPI(client)
	c.Now = func() time.Time { return time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC) }
	c.Observe = observe
	return c.Collect(context.Background(), source.Repo{Type: "gitlab", Slug: "acme/team/app", Visibility: "private", ForgeRepo: &p})
}

func project() gitlab.Project {
	t := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	return gitlab.Project{ID: 9, Path: "app", PathWithNamespace: "acme/team/app", DefaultBranch: "main", LastActivityAt: &t}
}

func TestGitLabCollect(t *testing.T) {
	f := &glFake{
		trees: map[string]any{
			"": `[{"path":"README.md","type":"blob","mode":"100644"},{"path":"LICENSE","type":"blob","mode":"100644"},
				{"path":".gitlab-ci.yml","type":"blob","mode":"100644"},{"path":"link","type":"blob","mode":"120000"},
				{"path":"vendor","type":"commit","mode":"160000"},{"path":"docs","type":"tree","mode":"040000"}]`,
			"docs": `[{"path":"docs/CODEOWNERS","type":"blob","mode":"100644"}]`,
		},
		files: map[string]any{"README.md": "# Title\n" + strings.Repeat("x", 5000) + "\xff"},
	}
	r := glCollect(t, f, project(), nil)
	if fmt.Sprint(r.FS.Files) != "[.gitlab-ci.yml LICENSE README.md docs/CODEOWNERS]" {
		t.Errorf("files = %v: want blobs only, no symlink or submodule", r.FS.Files)
	}
	if !r.FS.KeyFiles["CODEOWNERS"] || !r.FS.KeyFiles["LICENSE"] || len(r.FS.CIFiles) != 1 {
		t.Errorf("key files %v, CI files %v", r.FS.KeyFiles, r.FS.CIFiles)
	}
	if r.FS.ReadmeContent == nil || len(*r.FS.ReadmeContent) != maxReadmeBytes || r.FS.ReadmeUnread || len(r.FS.UnreadDirs) != 0 {
		t.Errorf("readme %d bytes, unread %v %v", len(*r.FS.ReadmeContent), r.FS.ReadmeUnread, r.FS.UnreadDirs)
	}
	if r.SourceType != models.SourceGitLab || r.Visibility != "private" || r.Name != "app" || r.Platform != nil {
		t.Errorf("repo = %+v", r)
	}
	if *r.Git.DefaultBranch != "main" || *r.Git.DaysSinceCommit != 7 || r.Git.IsStale || fmt.Sprint(r.Git.Branches) != "[main dev]" {
		t.Errorf("git = %+v", r.Git)
	}
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := project()
	p.LastActivityAt = &old
	if r := glCollect(t, f, p, nil); !r.Git.IsStale {
		t.Error("last activity 280 days ago is stale")
	}
	p.LastActivityAt = nil
	if r := glCollect(t, f, p, nil); r.Git.LastCommitAt != nil || r.Git.IsStale {
		t.Errorf("no last activity: %+v", r.Git)
	}
}

// A failed listing is unread, not absent; a 404 is absent; a listing over the
// page cap is unread; a listed README that cannot be read is unread. Each
// failure reaches the observer.
func TestGitLabCollectUnreadEvidence(t *testing.T) {
	var observed atomic.Int32
	f := &glFake{
		trees: map[string]any{
			"":                  `[{"path":"README.md","type":"blob","mode":"100644"}]`,
			".github":           http.StatusInternalServerError,
			"docs":              http.StatusForbidden,
			".circleci":         http.StatusTooManyRequests,
			".github/workflows": endless{},
		},
		files: map[string]any{"README.md": http.StatusInternalServerError},
	}
	r := glCollect(t, f, project(), func(error) { observed.Add(1) })
	if fmt.Sprint(r.FS.UnreadDirs) != "[.github .github/workflows .circleci docs]" || !r.FS.ReadmeUnread || r.FS.ReadmeContent != nil {
		t.Errorf("unread dirs %v, readme unread %v", r.FS.UnreadDirs, r.FS.ReadmeUnread)
	}
	if observed.Load() != 4 {
		t.Errorf("observed %d errors, want 4", observed.Load())
	}
}

// GitLab also reads CODEOWNERS from .gitlab/, so it counts there; when
// .gitlab/ cannot be read, a missing CODEOWNERS is not shown absent.
func TestGitLabCodeownersInDotGitlab(t *testing.T) {
	r := glCollect(t, &glFake{trees: map[string]any{".gitlab": `[{"path":".gitlab/CODEOWNERS","type":"blob","mode":"100644"}]`}}, project(), nil)
	if !r.FS.KeyFiles["CODEOWNERS"] {
		t.Errorf("key files = %v", r.FS.KeyFiles)
	}
	r = glCollect(t, &glFake{trees: map[string]any{".gitlab": http.StatusInternalServerError}}, project(), nil)
	if fmt.Sprint(r.FS.UnreadDirs) != "[.gitlab]" {
		t.Errorf("unread = %v", r.FS.UnreadDirs)
	}
}

// An empty repository has nothing to read: no tree is requested, and nothing
// is unread.
func TestGitLabCollectEmptyRepo(t *testing.T) {
	f := &glFake{}
	r := glCollect(t, f, gitlab.Project{ID: 9, Path: "app", EmptyRepo: true}, nil)
	if f.treeHits.Load() != 0 || len(r.FS.Files) != 0 || len(r.FS.UnreadDirs) != 0 || r.FS.ReadmeUnread {
		t.Errorf("%d tree requests, files %v, unread %v", f.treeHits.Load(), r.FS.Files, r.FS.UnreadDirs)
	}
}

// GitLab leaves out the default branch when the token cannot read the
// repository. That is unread evidence, not an empty repository, so the
// checks report unknown rather than failing.
func TestGitLabCollectNoDefaultBranchIsUnread(t *testing.T) {
	f := &glFake{}
	r := glCollect(t, f, gitlab.Project{ID: 9, Path: "app"}, nil)
	if f.treeHits.Load() != 0 || fmt.Sprint(r.FS.UnreadDirs) != fmt.Sprint(gitlabEvidenceDirs) || !r.FS.ReadmeUnread {
		t.Errorf("%d tree requests, unread %v, readme unread %v", f.treeHits.Load(), r.FS.UnreadDirs, r.FS.ReadmeUnread)
	}
}

func TestGitLabCollectWaivers(t *testing.T) {
	root := `[{"path":".baseliner.yml","type":"blob","mode":"100644"}]`
	for name, c := range map[string]struct {
		body any
		want int
	}{
		"valid":      {"waivers:\n  - check: ci_present\n    reason: docs only\n", 1},
		"oversized":  {"waivers:\n  - check: ci_present\n    reason: x\n#" + strings.Repeat("x", waivers.MaxBytes), 0},
		"unreadable": {http.StatusInternalServerError, 0},
		"invalid":    {"waivers: [", 0},
	} {
		t.Run(name, func(t *testing.T) {
			r := glCollect(t, &glFake{trees: map[string]any{"": root}, files: map[string]any{".baseliner.yml": c.body}}, project(), nil)
			if len(r.Waivers) != c.want {
				t.Errorf("waivers = %+v, want %d", r.Waivers, c.want)
			}
		})
	}
	// A symlink at the waiver path is not a file in the listing, so it is
	// never read.
	r := glCollect(t, &glFake{trees: map[string]any{"": `[{"path":".baseliner.yml","type":"blob","mode":"120000"}]`},
		files: map[string]any{".baseliner.yml": "waivers:\n  - check: ci_present\n    reason: x\n"}}, project(), nil)
	if r.Waivers != nil {
		t.Errorf("a symlinked waiver file was read: %+v", r.Waivers)
	}
}

// A custom CI configuration path decides ci_present: in the repo when the file
// exists, in another project or at a URL as configured CI (never named), and a
// root .gitlab-ci.yml no longer counts, as GitLab ignores it then.
func TestGitLabCollectCIConfigPath(t *testing.T) {
	root := `[{"path":".gitlab-ci.yml","type":"blob","mode":"100644"}]`
	for name, c := range map[string]struct {
		path     string
		file     any // the custom file's response
		wantCI   []string
		wantDirs []string
	}{
		"default":          {"", nil, []string{".gitlab-ci.yml"}, nil},
		"explicit default": {".gitlab-ci.yml", nil, []string{".gitlab-ci.yml"}, nil},
		"in repo, present": {"ci/pipeline.yml", "stages: [test]\n", []string{"ci/pipeline.yml"}, nil},
		"in repo, missing": {"ci/pipeline.yml", nil, nil, nil},
		"in repo, unread":  {"ci/pipeline.yml", http.StatusInternalServerError, nil, []string{"ci"}},
		"at the root":      {"pipeline.yml", http.StatusForbidden, nil, []string{""}},
		"another project":  {"ci.yml@group/private-templates", nil, []string{externalCI}, nil},
		"at a URL":         {"https://ci.example.com/pipeline.yml", nil, []string{externalCI}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			p := project()
			p.CIConfigPath = c.path
			r := glCollect(t, &glFake{trees: map[string]any{"": root}, files: map[string]any{"ci%2Fpipeline.yml": c.file, "pipeline.yml": c.file}}, p, nil)
			if strings.Join(r.FS.CIFiles, ",") != strings.Join(c.wantCI, ",") {
				t.Errorf("CI files = %v, want %v", r.FS.CIFiles, c.wantCI)
			}
			if strings.Join(r.FS.UnreadDirs, ",") != strings.Join(c.wantDirs, ",") {
				t.Errorf("unread = %v, want %v", r.FS.UnreadDirs, c.wantDirs)
			}
			for _, f := range r.FS.CIFiles {
				if strings.Contains(f, "private-templates") {
					t.Errorf("another project's path is carried: %v", r.FS.CIFiles)
				}
			}
		})
	}
}
