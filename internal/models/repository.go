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
	Files    []string        `json:"files"`
	KeyFiles map[string]bool `json:"key_files"` // README, LICENSE, GITIGNORE, CODEOWNERS
	// ReadmeContent is nil when no README exists (distinct from an empty
	// README), matching the Python Optional[str] semantics the checks rely on.
	ReadmeContent *string  `json:"readme_content"`
	CIFiles       []string `json:"ci_files"`
	// DisabledCIFiles maps each CI file GitHub reports as disabled to its
	// workflow state (e.g. disabled_inactivity). Nil means the state is
	// unknown (no Actions read access, or a local checkout), and ci_present
	// falls back to file presence; an empty map means none are disabled.
	DisabledCIFiles map[string]string `json:"disabled_ci_files,omitempty"`
	DepUpdateFiles  []string          `json:"dep_update_files"`
}

// GitContext holds git metadata used by git checks.
type GitContext struct {
	// DefaultBranch is nil when unknown (no origin/HEAD and no checked-out HEAD).
	DefaultBranch *string    `json:"default_branch"`
	LastCommitAt  *time.Time `json:"last_commit_at"`
	// DaysSinceCommit is nil when the commit time is unknown.
	DaysSinceCommit *int     `json:"days_since_commit"`
	Branches        []string `json:"branches"`
	IsStale         bool     `json:"is_stale"`
}

// SourceState is what one platform source said when it was read.
type SourceState string

const (
	// SourcePresent means the source was read and returned configuration.
	SourcePresent SourceState = "present"
	// SourceAbsent means the source was read and positively reported nothing
	// (e.g. classic protection's 404 "Branch not protected").
	SourceAbsent SourceState = "absent"
	// SourceUnreadable means the source could not be read: a plan gate (403),
	// missing permission, or any response that is not a positive answer.
	// It is never treated as absent.
	SourceUnreadable SourceState = "unreadable"
)

// ClassicProtection is classic branch protection on the default branch, from
// GET /repos/{o}/{r}/branches/{b}/protection.
type ClassicProtection struct {
	State             SourceState `json:"state"`
	RequiredApprovals int         `json:"required_approvals"`
	EnforceAdmins     bool        `json:"enforce_admins"`
	Error             string      `json:"error,omitempty"`
}

// BypassActor is one ruleset bypass actor. Mode is always, pull_request or
// exempt; exempt also suppresses the bypass audit entry.
//
// Only the mode is kept. GitHub shows bypass actors to admins only, so who can
// bypass is never recorded, and so can never be republished in output, which
// may be public.
type BypassActor struct {
	Mode string `json:"mode"`
}

// BranchRuleset is a ruleset whose rules apply to the default branch.
type BranchRuleset struct {
	ID                int64  `json:"id"`
	Name              string `json:"name"`
	SourceType        string `json:"source_type"` // Repository | Organization | Enterprise
	RequiredApprovals int    `json:"required_approvals"`
	// BypassState is SourceUnreadable when the ruleset object could not be read
	// or omitted bypass_actors (GitHub omits the key, rather than returning an
	// empty list, for callers without admin access). BypassError says which.
	BypassState  SourceState   `json:"bypass_state"`
	BypassError  string        `json:"bypass_error,omitempty"`
	BypassActors []BypassActor `json:"bypass_actors"`
}

// RulesView is the rules that apply to the default branch, from
// GET /repos/{o}/{r}/rules/branches/{b}. It never mentions classic protection.
type RulesView struct {
	State    SourceState     `json:"state"`
	Rulesets []BranchRuleset `json:"rulesets"`
	Error    string          `json:"error,omitempty"`
}

// PlatformContext is forge metadata that file presence cannot show: what
// protects the default branch, read from both GitHub sources because neither
// reports the other.
type PlatformContext struct {
	DefaultBranch string            `json:"default_branch"`
	Classic       ClassicProtection `json:"classic"`
	Rules         RulesView         `json:"rules"`
}

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
