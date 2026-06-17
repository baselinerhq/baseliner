// Prototype: replicate baseliner's GitCollector using go-git.
// Goal: prove go-git can extract default branch (from origin/HEAD),
// last-commit time, staleness, and branch list from a local clone.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

const staleThresholdDays = 90

type GitContext struct {
	DefaultBranch   string   `json:"default_branch"`
	LastCommitAt    string   `json:"last_commit_at"`
	DaysSinceCommit int      `json:"days_since_commit"`
	IsStale         bool     `json:"is_stale"`
	Branches        []string `json:"branches"`
}

// detectDefaultBranch mirrors GitCollector: prefer the symbolic ref
// refs/remotes/origin/HEAD, fall back to the checked-out HEAD branch.
func detectDefaultBranch(r *git.Repository) (string, error) {
	if ref, err := r.Reference(plumbing.ReferenceName("refs/remotes/origin/HEAD"), false); err == nil {
		if ref.Type() == plumbing.SymbolicReference {
			target := ref.Target().String() // e.g. refs/remotes/origin/main
			parts := strings.Split(target, "/")
			return parts[len(parts)-1], nil
		}
	}
	head, err := r.Head()
	if err != nil {
		return "", err
	}
	return head.Name().Short(), nil
}

func collect(path string, now time.Time) (*GitContext, error) {
	r, err := git.PlainOpen(path)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}

	defaultBranch, err := detectDefaultBranch(r)
	if err != nil {
		return nil, fmt.Errorf("default branch: %w", err)
	}

	head, err := r.Head()
	if err != nil {
		return nil, fmt.Errorf("head: %w", err)
	}
	commit, err := r.CommitObject(head.Hash())
	if err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	when := commit.Committer.When.UTC()
	days := int(now.UTC().Sub(when).Hours() / 24)

	var branches []string
	iter, err := r.Branches()
	if err != nil {
		return nil, fmt.Errorf("branches: %w", err)
	}
	_ = iter.ForEach(func(ref *plumbing.Reference) error {
		branches = append(branches, ref.Name().Short())
		return nil
	})

	return &GitContext{
		DefaultBranch:   defaultBranch,
		LastCommitAt:    when.Format(time.RFC3339),
		DaysSinceCommit: days,
		IsStale:         days > staleThresholdDays,
		Branches:        branches,
	}, nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: gitcollector <repo-path>...")
		os.Exit(2)
	}
	now := time.Now()
	out := map[string]any{}
	for _, path := range os.Args[1:] {
		ctx, err := collect(path, now)
		if err != nil {
			out[path] = map[string]string{"error": err.Error()}
			continue
		}
		out[path] = ctx
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}
