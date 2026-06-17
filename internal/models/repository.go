package models

import "time"

// SourceType identifies where a repo's metadata was collected from.
type SourceType string

const (
	SourceLocal  SourceType = "local"
	SourceGitHub SourceType = "github"
)

// FilesystemContext holds file-presence metadata used by hygiene checks.
// Both the filesystem collector and the GitHub-API collector populate this
// same shape so the checks are source-agnostic.
type FilesystemContext struct {
	Files          []string        `json:"files"`
	KeyFiles       map[string]bool `json:"key_files"` // README, LICENSE, GITIGNORE, CODEOWNERS
	ReadmeContent  string          `json:"readme_content"`
	CIFiles        []string        `json:"ci_files"`
	DepUpdateFiles []string        `json:"dep_update_files"`
}

// GitContext holds git metadata used by git checks.
type GitContext struct {
	DefaultBranch   string     `json:"default_branch"`
	LastCommitAt    *time.Time `json:"last_commit_at"`
	DaysSinceCommit int        `json:"days_since_commit"`
	Branches        []string   `json:"branches"`
	IsStale         bool       `json:"is_stale"`
}

// PlatformContext is reserved for richer platform metadata (branch
// protection, repo settings) in a later version.
type PlatformContext struct{}

// NormalizedRepository is the unified representation that lets the same
// checks run over local-git and GitHub-API sources. A nil context means
// that layer was not collected, and checks requiring it are skipped.
type NormalizedRepository struct {
	SourceType SourceType         `json:"source_type"`
	Slug       string             `json:"slug"`
	Name       string             `json:"name"`
	FS         *FilesystemContext `json:"fs,omitempty"`
	Git        *GitContext        `json:"git,omitempty"`
	Platform   *PlatformContext   `json:"platform,omitempty"`
}
