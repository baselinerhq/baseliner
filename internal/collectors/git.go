package collectors

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/baselinerhq/baseliner/internal/models"
	"github.com/baselinerhq/baseliner/internal/source"
)

const defaultStaleThresholdDays = 90

// Git collects a GitContext from a local repository via go-git.
type Git struct {
	StaleThresholdDays int
	Now                func() time.Time
}

// NewGit returns a Git collector with the default 90-day stale threshold.
func NewGit() Git {
	return Git{StaleThresholdDays: defaultStaleThresholdDays, Now: time.Now}
}

// Collect returns the git context for a local repo, or nil if the path is not a
// git repository or git metadata cannot be read (matching Python's "return None",
// which leaves the repo with only filesystem context so git checks skip).
func (g Git) Collect(src source.Repo) *models.GitContext {
	if src.Path == "" {
		return nil
	}
	root, err := filepath.Abs(src.Path)
	if err != nil {
		return nil
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		return nil
	}

	repo, err := gogit.PlainOpen(root)
	if err != nil {
		slog.Warn("failed to open git repo", "path", root, "err", err)
		return nil
	}
	head, err := repo.Head()
	if err != nil {
		slog.Warn("failed to read git HEAD", "path", root, "err", err)
		return nil
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		slog.Warn("failed to read HEAD commit", "path", root, "err", err)
		return nil
	}

	when := commit.Committer.When.UTC()
	days := int(g.now().Sub(when).Hours() / 24)

	return &models.GitContext{
		DefaultBranch:   detectDefaultBranch(repo),
		LastCommitAt:    &when,
		DaysSinceCommit: &days,
		Branches:        localBranchNames(repo),
		IsStale:         days > g.StaleThresholdDays,
	}
}

func (g Git) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

// detectDefaultBranch reads the symbolic ref refs/remotes/origin/HEAD and returns
// its target branch name, or nil if absent (Python does not fall back to HEAD).
func detectDefaultBranch(repo *gogit.Repository) *string {
	ref, err := repo.Reference(plumbing.ReferenceName("refs/remotes/origin/HEAD"), false)
	if err != nil || ref.Type() != plumbing.SymbolicReference {
		return nil
	}
	target := ref.Target().String() // refs/remotes/origin/<branch>
	idx := strings.LastIndex(target, "/")
	name := target[idx+1:]
	return &name
}

func localBranchNames(repo *gogit.Repository) []string {
	var names []string
	iter, err := repo.Branches()
	if err != nil {
		return names
	}
	_ = iter.ForEach(func(ref *plumbing.Reference) error {
		names = append(names, ref.Name().Short())
		return nil
	})
	return names
}
