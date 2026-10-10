package collectors

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/baselinerhq/baseliner/internal/gitea"
	"github.com/baselinerhq/baseliner/internal/source"
)

// TestLiveGiteaCollect runs the collector against a real Gitea or Forgejo
// instance when BASELINER_FORGEJO_URL and BASELINER_FORGEJO_TOKEN are set
// (skipped otherwise), on the organisation "sbx" described in
// internal/gitea/live_test.go, which also has an empty private repo "blank"
// and a ".forgejo/workflows/ci.yml" in "open-kit".
func TestLiveGiteaCollect(t *testing.T) {
	base, token := os.Getenv("BASELINER_FORGEJO_URL"), os.Getenv("BASELINER_FORGEJO_TOKEN")
	if base == "" || token == "" {
		t.Skip("BASELINER_FORGEJO_URL and BASELINER_FORGEJO_TOKEN not set")
	}
	client, err := gitea.New(base, token)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	repos, _, err := client.Repos(ctx, "org", "sbx", 10)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]*gitea.Repo{}
	for i := range repos {
		byName[repos[i].Name] = &repos[i]
	}
	col := NewGiteaAPI(client)
	for _, n := range []string{"secret-lab", "open-kit", "blank"} {
		if byName[n] == nil {
			t.Fatalf("no repo %s", n)
		}
	}

	secret := col.Collect(ctx, source.Repo{Type: "gitea", Slug: "sbx/secret-lab", Visibility: gitea.Visibility(*byName["secret-lab"]), ForgeRepo: byName["secret-lab"]})
	fs := secret.FS
	if !fs.KeyFiles["LICENSE"] || !fs.KeyFiles["CODEOWNERS"] || !fs.KeyFiles["README"] || len(fs.UnreadDirs) != 0 || fs.ReadmeUnread {
		t.Errorf("secret-lab: key files %v, unread %v, readme unread %v", fs.KeyFiles, fs.UnreadDirs, fs.ReadmeUnread)
	}
	if len(secret.Waivers) != 1 || secret.Visibility != "private" || secret.Git.LastCommitAt == nil || len(secret.Git.Branches) < 4 {
		t.Errorf("secret-lab: waivers %v, visibility %s, last commit %v, branches %v", secret.Waivers, secret.Visibility, secret.Git.LastCommitAt, secret.Git.Branches)
	}

	open := col.Collect(ctx, source.Repo{Type: "gitea", Slug: "sbx/open-kit", ForgeRepo: byName["open-kit"]})
	if strings.Join(open.FS.CIFiles, ",") != ".forgejo/workflows/ci.yml" || len(open.FS.UnreadDirs) != 0 {
		t.Errorf("open-kit: CI files %v, unread %v", open.FS.CIFiles, open.FS.UnreadDirs)
	}

	blank := col.Collect(ctx, source.Repo{Type: "gitea", Slug: "sbx/blank", ForgeRepo: byName["blank"]})
	if len(blank.FS.Files) != 0 || len(blank.FS.UnreadDirs) != 0 || blank.FS.ReadmeUnread {
		t.Errorf("blank: files %v, unread %v", blank.FS.Files, blank.FS.UnreadDirs)
	}
}
