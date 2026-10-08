// Package source defines RepoSource, the output of discovery and input to collectors.
package source

// Repo is a repository to scan, discovered locally or on a forge.
type Repo struct {
	Type string // "local", "github", or another forge's name
	Slug string
	Path string // set for local sources
	// GitHubRepo holds the *github.Repository for github sources (typed as any to
	// keep this package free of the go-github dependency).
	GitHubRepo any
	// ForgeRepo holds the forge's own record of the repo for other forges.
	ForgeRepo any
	// Visibility is set at discovery by forges other than GitHub: "public",
	// "internal" or "private". Empty is treated as private.
	Visibility string
	// Aliases are other spellings of Slug that logs or API errors may use; the
	// privacy guard protects them as it does Slug.
	Aliases []string
}
