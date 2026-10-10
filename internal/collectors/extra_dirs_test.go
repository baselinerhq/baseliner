package collectors

import (
	"context"
	"fmt"
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
	"github.com/baselinerhq/baseliner/internal/models"
	"github.com/baselinerhq/baseliner/internal/source"
)

// Each forge's collector lists a policy's extra directories beside its own,
// and records one it could not read as unread, not as empty. What it finds
// there is the policy's evidence only: a LICENSE in an extra directory, or
// one that cannot be read, never changes a built-in check.
func TestCollectorsListExtraDirs(t *testing.T) {
	extra := []string{"config", "ops/ci"}
	check := func(t *testing.T, fs *models.FilesystemContext) {
		t.Helper()
		if !slices.Contains(fs.PolicyFiles, "config/renovate.json") {
			t.Errorf("policy files %v lack config/renovate.json", fs.PolicyFiles)
		}
		if !slices.Equal(fs.PolicyUnreadDirs, []string{"ops/ci"}) {
			t.Errorf("policy unread %v, want [ops/ci]", fs.PolicyUnreadDirs)
		}
		if len(fs.Files) != 0 || len(fs.UnreadDirs) != 0 || fs.KeyFiles["LICENSE"] {
			t.Errorf("built-in evidence changed: files %v, unread %v, LICENSE %v", fs.Files, fs.UnreadDirs, fs.KeyFiles["LICENSE"])
		}
	}

	t.Run("github", func(t *testing.T) {
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/repos/o/r/contents/config":
				_, _ = w.Write([]byte(`[{"type":"file","name":"renovate.json","path":"config/renovate.json"},{"type":"file","name":"LICENSE","path":"config/LICENSE"}]`))
			case "/repos/o/r/contents/ops/ci":
				http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
			default:
				http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
			}
		})
		repo := &github.Repository{Owner: &github.User{Login: github.Ptr("o")}, Name: github.Ptr("r"), DefaultBranch: github.Ptr("main")}
		col := GitHubAPI{Client: fakeGitHubClient(t, h), StaleThresholdDays: 90, ExtraDirs: extra}
		got := col.Collect(context.Background(), source.Repo{Type: "github", Slug: "o/r", GitHubRepo: repo})
		check(t, got.FS)
	})

	t.Run("gitlab", func(t *testing.T) {
		f := &glFake{trees: map[string]any{
			"":       `[]`,
			"config": `[{"path":"config/renovate.json","type":"blob","mode":"100644"},{"path":"config/LICENSE","type":"blob","mode":"100644"}]`,
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
		check(t, got.FS)
		// With no default branch nothing is read, the extra directories too.
		p.DefaultBranch = ""
		got = c.Collect(context.Background(), source.Repo{Type: "gitlab", Slug: "acme/team/app", ForgeRepo: &p})
		if !slices.Equal(got.FS.PolicyUnreadDirs, extra) || slices.Contains(got.FS.UnreadDirs, "config") {
			t.Errorf("no default branch: policy unread %v, unread %v", got.FS.PolicyUnreadDirs, got.FS.UnreadDirs)
		}
	})

	t.Run("gitea", func(t *testing.T) {
		f := &gtFake{dirs: map[string]any{
			"":       `[]`,
			"config": `[{"path":"config/renovate.json","type":"file"},{"path":"config/LICENSE","type":"file"}]`,
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
		check(t, got.FS)
		r.DefaultBranch = ""
		got = c.Collect(context.Background(), source.Repo{Type: "gitea", Slug: "o/r", ForgeRepo: &r})
		if !slices.Equal(got.FS.PolicyUnreadDirs, extra) || slices.Contains(got.FS.UnreadDirs, "config") {
			t.Errorf("no default branch: policy unread %v, unread %v", got.FS.PolicyUnreadDirs, got.FS.UnreadDirs)
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
	if !slices.Contains(got.PolicyFiles, "config/renovate.json") {
		t.Errorf("files %v lack config/renovate.json", got.Files)
	}
	if strings.Join(got.PolicyUnreadDirs, ",") != "ops,ops/ci" || strings.Join(got.UnreadDirs, ",") != "ops" {
		t.Errorf("policy unread %v, want ops and ops/ci; unread %v, want ops", got.PolicyUnreadDirs, got.UnreadDirs)
	}
}

// GitHub lists at most 1000 entries of a directory, so a listing that long
// is incomplete: what it lists is present, the rest unknown.
func TestGitHubListingAtCapIsUnread(t *testing.T) {
	var big strings.Builder
	big.WriteString(`[{"type":"file","path":"config/renovate.json"}`)
	for i := 1; i < maxContentsEntries; i++ {
		fmt.Fprintf(&big, `,{"type":"file","path":"config/f%d"}`, i)
	}
	big.WriteString("]")
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/o/r/contents/config" {
			_, _ = w.Write([]byte(big.String()))
			return
		}
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})
	repo := &github.Repository{Owner: &github.User{Login: github.Ptr("o")}, Name: github.Ptr("r"), DefaultBranch: github.Ptr("main")}
	col := GitHubAPI{Client: fakeGitHubClient(t, h), StaleThresholdDays: 90, ExtraDirs: []string{"config"}}
	got := col.Collect(context.Background(), source.Repo{Type: "github", Slug: "o/r", GitHubRepo: repo}).FS
	if !slices.Equal(got.PolicyUnreadDirs, []string{"config"}) || !slices.Contains(got.PolicyFiles, "config/renovate.json") {
		t.Errorf("policy unread %v; renovate.json listed %v", got.PolicyUnreadDirs, slices.Contains(got.PolicyFiles, "config/renovate.json"))
	}
}

// Gitea answers a missing directory and a missing ref with the same 404, so
// the root is listed first: once it proves the ref, an extra directory's 404
// is absence, not unread.
func TestGiteaExtraDirAfterRoot(t *testing.T) {
	f := &gtFake{dirs: map[string]any{"": `[]`}}
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(srv.Close)
	client, err := gitea.New(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	c := NewGiteaAPI(client)
	c.ExtraDirs = []string{"config"}
	r := giteaRepo()
	got := c.Collect(context.Background(), source.Repo{Type: "gitea", Slug: "o/r", ForgeRepo: &r}).FS
	if len(got.PolicyUnreadDirs) != 0 {
		t.Errorf("policy unread %v, want none: the root proved the ref", got.PolicyUnreadDirs)
	}
}
