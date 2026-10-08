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
	// InactiveCIFiles maps each GitHub Actions workflow file that is not
	// running to why: its disabled state (e.g. disabled_inactivity), or that
	// GitHub does not list it at all (a fork whose Actions were never
	// enabled). Nil means the state is unknown (no Actions read access, or a
	// local checkout), and ci_present falls back to file presence; an empty
	// map means every workflow file is active.
	InactiveCIFiles map[string]string `json:"inactive_ci_files,omitempty"`
	DepUpdateFiles  []string          `json:"dep_update_files"`
	// UnreadDirs lists repo-relative directories ("" is the root) whose
	// listing failed for a reason other than absence. Files there are
	// unknown, so a check that fails for want of a file that could live
	// there reports unknown instead.
	UnreadDirs []string `json:"unread_dirs,omitempty"`
	// ReadmeUnread reports that the README exists or may exist but its
	// content could not be read; the README-content checks then report
	// unknown instead of failing.
	ReadmeUnread bool `json:"readme_unread,omitempty"`
}

// GitContext holds git metadata used by git checks.
type GitContext struct {
	// DefaultBranch is nil when unknown: locally, when refs/remotes/origin/HEAD
	// is not set (the checked-out branch is not used as a fallback).
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
	// Visibility is the forge's visibility for the repo (public, private or
	// internal), or "" where there is none, as for a local checkout.
	Visibility string `json:"visibility,omitempty"`
	// Waivers are the waivers the repo declares about itself in its
	// .baseliner.yml. The central policy decides whether they apply.
	Waivers []Waiver `json:"waivers,omitempty"`
}

// Waiver exempts one check for one repo, with a reason and an optional last
// day.
type Waiver struct {
	Check  string     `json:"check"`
	Reason string     `json:"reason"`
	Until  *time.Time `json:"until,omitempty"` // last day it applies, in UTC; nil never expires
}

// Active reports whether the waiver still applies at now.
func (w Waiver) Active(now time.Time) bool {
	if w.Until == nil {
		return true
	}
	return !now.UTC().After(w.Until.Add(24*time.Hour - time.Nanosecond))
}
