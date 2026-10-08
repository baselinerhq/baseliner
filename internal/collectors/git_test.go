package collectors

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/baselinerhq/baseliner/internal/source"
)

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e.x",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e.x",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// A default branch whose name contains '/' must be preserved (not truncated at
// the last slash) — matching GitPython's remote_head.
func TestGitCollectSlashDefaultBranch(t *testing.T) {
	root := t.TempDir()
	gitCmd(t, root, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(root, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, root, "add", "-A")
	gitCmd(t, root, "commit", "-qm", "init")
	gitCmd(t, root, "update-ref", "refs/remotes/origin/feature/x", "HEAD")
	gitCmd(t, root, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/feature/x")

	g := Git{StaleThresholdDays: 90, Now: time.Now}
	ctx := g.Collect(source.Repo{Type: "local", Slug: root, Path: root})
	if ctx == nil || ctx.DefaultBranch == nil {
		t.Fatal("expected git context with default branch")
	}
	if *ctx.DefaultBranch != "feature/x" {
		t.Errorf("default branch = %q, want feature/x", *ctx.DefaultBranch)
	}
}

func TestGitCollectWithOriginHead(t *testing.T) {
	root := t.TempDir()
	gitCmd(t, root, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(root, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, root, "add", "-A")
	gitCmd(t, root, "commit", "-qm", "init")
	// Fabricate a remote-tracking default branch pointer.
	gitCmd(t, root, "update-ref", "refs/remotes/origin/main", "HEAD")
	gitCmd(t, root, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")

	g := Git{StaleThresholdDays: 90, Now: func() time.Time { return time.Now() }}
	ctx := g.Collect(source.Repo{Type: "local", Slug: root, Path: root})
	if ctx == nil {
		t.Fatal("expected git context")
	}
	if ctx.DefaultBranch == nil || *ctx.DefaultBranch != "main" {
		t.Errorf("default branch = %v, want main", ctx.DefaultBranch)
	}
	if ctx.IsStale {
		t.Error("fresh commit should not be stale")
	}
	if ctx.DaysSinceCommit == nil || *ctx.DaysSinceCommit != 0 {
		t.Errorf("days = %v, want 0", ctx.DaysSinceCommit)
	}
}

func TestGitCollectNonRepo(t *testing.T) {
	if ctx := NewGit().Collect(source.Repo{Type: "local", Slug: "x", Path: t.TempDir()}); ctx != nil {
		t.Error("non-git dir should yield nil git context")
	}
}

func TestGitCollectNoOriginHead(t *testing.T) {
	root := t.TempDir()
	gitCmd(t, root, "init", "-q", "-b", "trunk")
	if err := os.WriteFile(filepath.Join(root, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, root, "add", "-A")
	gitCmd(t, root, "commit", "-qm", "init")
	ctx := NewGit().Collect(source.Repo{Type: "local", Slug: root, Path: root})
	if ctx == nil {
		t.Fatal("expected git context")
	}
	// No origin/HEAD -> nil default branch (parity with Python; no fallback to HEAD).
	if ctx.DefaultBranch != nil {
		t.Errorf("default branch = %v, want nil", *ctx.DefaultBranch)
	}
}

// In a linked worktree .git is a file pointing at <clone>/.git/worktrees/<name>;
// HEAD is per-worktree, while objects and refs live in the clone's common dir
// (#111). The clone's only commit is old and the worktree's is fresh, so reading
// the clone's HEAD instead of the worktree's would report the worktree stale.
func TestGitCollectLinkedWorktree(t *testing.T) {
	clone := t.TempDir()
	gitCmd(t, clone, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(clone, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, clone, "add", "-A")
	old := time.Now().AddDate(0, 0, -200).Format(time.RFC3339)
	t.Setenv("GIT_COMMITTER_DATE", old)
	gitCmd(t, clone, "commit", "-qm", "old")
	gitCmd(t, clone, "update-ref", "refs/remotes/origin/main", "HEAD")
	gitCmd(t, clone, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")

	wt := filepath.Join(t.TempDir(), "wt")
	gitCmd(t, clone, "worktree", "add", "-q", "-b", "topic", wt)
	if fi, err := os.Lstat(filepath.Join(wt, ".git")); err != nil || fi.IsDir() {
		t.Fatalf("expected %s/.git to be a file (linked worktree), err=%v", wt, err)
	}
	t.Setenv("GIT_COMMITTER_DATE", time.Now().Format(time.RFC3339))
	gitCmd(t, wt, "commit", "-q", "--allow-empty", "-m", "fresh")

	g := Git{StaleThresholdDays: 90, Now: time.Now}
	ctx := g.Collect(source.Repo{Type: "local", Slug: wt, Path: wt})
	if ctx == nil {
		t.Fatal("expected git context for a linked worktree")
	}
	if ctx.DefaultBranch == nil || *ctx.DefaultBranch != "main" {
		t.Errorf("default branch = %v, want main (from the shared origin/HEAD)", ctx.DefaultBranch)
	}
	if ctx.IsStale || ctx.DaysSinceCommit == nil || *ctx.DaysSinceCommit != 0 {
		t.Errorf("stale=%v days=%v: read the clone's HEAD, not the worktree's", ctx.IsStale, ctx.DaysSinceCommit)
	}
}
