package gitea

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestLiveForgejo runs the client against a real Gitea or Forgejo instance
// when BASELINER_FORGEJO_URL and BASELINER_FORGEJO_TOKEN are set (it is
// skipped otherwise). The instance must have an organisation "sbx" with more
// than one page of repos, a public "open-kit" with a README, and a private
// "secret-lab" with a LICENSE, .github/CODEOWNERS, docs/ and several
// branches. The fakes in gitea_test.go are modelled on what this returns.
func TestLiveForgejo(t *testing.T) {
	base, token := os.Getenv("BASELINER_FORGEJO_URL"), os.Getenv("BASELINER_FORGEJO_TOKEN")
	if base == "" || token == "" {
		t.Skip("BASELINER_FORGEJO_URL and BASELINER_FORGEJO_TOKEN not set")
	}
	c, err := New(base, token)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	repos, complete, err := c.Repos(ctx, "org", "sbx", 10)
	if err != nil || !complete || len(repos) <= pageSize {
		t.Fatalf("Repos: %d repos, complete %v, %v; want more than one page", len(repos), complete, err)
	}
	byName := map[string]Repo{}
	for _, r := range repos {
		byName[r.Name] = r
	}
	if Visibility(byName["secret-lab"]) != "private" || Visibility(byName["open-kit"]) != "public" {
		t.Errorf("visibility: secret-lab %q, open-kit %q", Visibility(byName["secret-lab"]), Visibility(byName["open-kit"]))
	}

	root, isDir, err := c.Contents(ctx, "sbx", "secret-lab", "main", "")
	if err != nil || !isDir || len(root) == 0 {
		t.Fatalf("root: %v %v %v", root, isDir, err)
	}
	if _, isDir, err := c.Contents(ctx, "sbx", "secret-lab", "main", "LICENSE"); err != nil || isDir {
		t.Errorf("a file's path: isDir %v, %v; want no directory and no error", isDir, err)
	}
	_, _, err = c.Contents(ctx, "sbx", "secret-lab", "main", ".circleci")
	if Status(err) != 404 {
		t.Errorf("missing dir: %v, want a 404", err)
	}
	if err != nil && strings.Contains(err.Error(), "secret-lab") {
		t.Errorf("the error names the repo: %v", err)
	}
	b, err := c.RawFile(ctx, "sbx", "secret-lab", "main", ".baseliner.yml", 1000)
	if err != nil || !strings.Contains(string(b), "waivers") {
		t.Errorf("raw waiver file: %q, %v", b, err)
	}
	if _, err := c.RawFile(ctx, "sbx", "secret-lab", "main", "nope.md", 10); Status(err) != 404 {
		t.Errorf("missing raw file: %v, want a 404", err)
	}
	branches, err := c.Branches(ctx, "sbx", "secret-lab", 100)
	if err != nil || len(branches) < 4 {
		t.Errorf("branches: %v, %v", branches, err)
	}
	when, err := c.LastCommit(ctx, "sbx", "secret-lab", "main")
	if err != nil || when.IsZero() {
		t.Errorf("last commit: %v, %v", when, err)
	}
}
