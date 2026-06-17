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
