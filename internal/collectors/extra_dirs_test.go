package collectors

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-github/v68/github"

	"github.com/baselinerhq/baseliner/internal/gitea"
	"github.com/baselinerhq/baseliner/internal/gitlab"
	"github.com/baselinerhq/baseliner/internal/source"
)

// Each forge's collector lists a policy's extra directories beside its own,
// and records one it could not read as unread, not as empty.
func TestCollectorsListExtraDirs(t *testing.T) {
	extra := []string{"config", "ops/ci"}
	check := func(t *testing.T, files, unread []string) {
		t.Helper()
		if !slices.Contains(files, "config/renovate.json") {
			t.Errorf("files %v lack config/renovate.json", files)
		}
		if !slices.Equal(unread, []string{"ops/ci"}) {
			t.Errorf("unread %v, want [ops/ci]", unread)
		}
	}

	t.Run("github", func(t *testing.T) {
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/repos/o/r/contents/config":
				_, _ = w.Write([]byte(`[{"type":"file","name":"renovate.json","path":"config/renovate.json"}]`))
			case "/repos/o/r/contents/ops/ci":
				http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
			default:
				http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
			}
		})
		repo := &github.Repository{Owner: &github.User{Login: github.Ptr("o")}, Name: github.Ptr("r"), DefaultBranch: github.Ptr("main")}
		col := GitHubAPI{Client: fakeGitHubClient(t, h), StaleThresholdDays: 90, ExtraDirs: extra}
		got := col.Collect(context.Background(), source.Repo{Type: "github", Slug: "o/r", GitHubRepo: repo})
		check(t, got.FS.Files, got.FS.UnreadDirs)
	})

	t.Run("gitlab", func(t *testing.T) {
		f := &glFake{trees: map[string]any{
			"":       `[]`,
			"config": `[{"path":"config/renovate.json","type":"blob","mode":"100644"}]`,
			"ops/ci": http.StatusInternalServerError,
		}}
		srv := httptest.NewServer(http.HandlerFunc(f.handler))
		t.Cleanup(srv.Close)
		client, err := gitlab.New(srv.URL, "tok")
		if err != nil {
			t.Fatal(err)
		}
		c := NewGitLabAPI(client)
		c.ExtraDirs = extra
		p := project()
		got := c.Collect(context.Background(), source.Repo{Type: "gitlab", Slug: "acme/team/app", ForgeRepo: &p})
		check(t, got.FS.Files, got.FS.UnreadDirs)
		// With no default branch nothing is read, the extra directories too.
		p.DefaultBranch = ""
		got = c.Collect(context.Background(), source.Repo{Type: "gitlab", Slug: "acme/team/app", ForgeRepo: &p})
		if !slices.Contains(got.FS.UnreadDirs, "ops/ci") || !slices.Contains(got.FS.UnreadDirs, "config") {
			t.Errorf("no default branch: unread %v lacks the extra directories", got.FS.UnreadDirs)
		}
	})

	t.Run("gitea", func(t *testing.T) {
		f := &gtFake{dirs: map[string]any{
			"":       `[]`,
			"config": `[{"path":"config/renovate.json","type":"file"}]`,
			"ops/ci": http.StatusInternalServerError,
		}}
		srv := httptest.NewServer(http.HandlerFunc(f.handler))
		t.Cleanup(srv.Close)
		client, err := gitea.New(srv.URL, "tok")
		if err != nil {
			t.Fatal(err)
		}
		c := NewGiteaAPI(client)
		c.ExtraDirs = extra
		r := giteaRepo()
		got := c.Collect(context.Background(), source.Repo{Type: "gitea", Slug: "o/r", ForgeRepo: &r})
		check(t, got.FS.Files, got.FS.UnreadDirs)
		r.DefaultBranch = ""
		got = c.Collect(context.Background(), source.Repo{Type: "gitea", Slug: "o/r", ForgeRepo: &r})
		if !slices.Contains(got.FS.UnreadDirs, "ops/ci") || !slices.Contains(got.FS.UnreadDirs, "config") {
			t.Errorf("no default branch: unread %v lacks the extra directories", got.FS.UnreadDirs)
		}
	})
}

// A local walk reads extra directories with the rest of the tree; one under
// a directory it could not read is unread too.
func TestFilesystemExtraDirsUnderUnreadable(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"config/renovate.json", "ops/ci/x.yml"} {
		p := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	unreadable(t, filepath.Join(root, "ops"))
	got := Filesystem{ExtraDirs: []string{"config", "ops/ci"}}.Collect(source.Repo{Type: "local", Slug: "x", Path: root}).FS
	if !slices.Contains(got.Files, "config/renovate.json") {
		t.Errorf("files %v lack config/renovate.json", got.Files)
	}
	if strings.Join(got.UnreadDirs, ",") != "ops,ops/ci" {
		t.Errorf("unread %v, want ops and ops/ci", got.UnreadDirs)
	}
}
