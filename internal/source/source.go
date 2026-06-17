// Package source defines RepoSource, the output of discovery and input to collectors.
package source

// Repo is a repository to scan, discovered locally or via GitHub.
type Repo struct {
	Type string // "local" or "github"
	Slug string
	Path string // set for local sources
	// GitHubRepo holds the *github.Repository for github sources (typed as any to
	// keep this package free of the go-github dependency).
	GitHubRepo any
}
