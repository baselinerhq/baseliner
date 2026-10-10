// Package checks implements the baseliner compliance checks and a registry.
//
// Each check declares a required layer (fs/git/platform). Evaluate applies the
// layer guard — if the required context is absent the check reports
// StatusUnknown (applicable but unobservable) rather than passing or failing —
// then runs the check's own logic. Severity on a result is left as "unknown";
// the policy engine overrides it from the policy definition.
package checks

import (
	"slices"

	"github.com/baselinerhq/baseliner/internal/models"
)

// Layer is the repository context a check requires.
type Layer string

const (
	LayerNone     Layer = ""
	LayerFS       Layer = "fs"
	LayerGit      Layer = "git"
	LayerPlatform Layer = "platform"
)

// Check is a single compliance rule.
type Check interface {
	ID() string
	Layer() Layer
	// Eval runs the rule. Evaluate guarantees the required layer is present.
	Eval(repo *models.NormalizedRepository) models.CheckResult
}

// Evaluate applies the layer guard then runs the check.
func Evaluate(c Check, repo *models.NormalizedRepository) models.CheckResult {
	switch c.Layer() {
	case LayerFS:
		if repo.FS == nil {
			return unobservable(c.ID(), "Filesystem context not available")
		}
		res := c.Eval(repo)
		if res.Status == models.StatusFail {
			if why := unreadEvidence(c.ID(), repo.FS); why != "" {
				return unobservable(c.ID(), why)
			}
		}
		return res
	case LayerGit:
		if repo.Git == nil {
			return unobservable(c.ID(), "Git context not available")
		}
	case LayerPlatform:
		if repo.Platform == nil {
			return unobservable(c.ID(), "Platform context not available")
		}
	}
	return c.Eval(repo)
}

// fsEvidence lists, per filesystem check, the listed directories ("" is the
// root) whose files it looks at. A check in nameMatched matches by file name in
// any listed directory instead: README, LICENSE and .gitignore by name, and
// ci_present because a Jenkinsfile counts wherever it is. An empty list means it reads no listing: the
// README-content checks read the README itself. Each directory is listed on
// its own, so an unread parent hides nothing in a child that was read.
var fsEvidence = map[string][]string{
	"readme_nonempty":          {},
	"readme_has_heading":       {},
	"codeowners_exists":        {"", ".github", "docs", ".gitlab", ".gitea", ".forgejo"},
	"dependency_update_config": {"", ".github"},
}

// nameMatched lists the filesystem checks whose files can be in any listed
// directory, so any unread directory can hide one.
var nameMatched = map[string]bool{
	"readme_exists": true, "license_exists": true, "gitignore_exists": true, "ci_present": true,
}

// readmeContentChecks read the README's content rather than a listing.
var readmeContentChecks = map[string]bool{"readme_nonempty": true, "readme_has_heading": true}

// unreadEvidence explains why a failing filesystem check cannot be trusted, or
// returns "" when its evidence was read in full. A failure means a file was not
// found; it proves absence only if every place the file could be was read. A
// pass stands regardless, since a file that was found is present.
func unreadEvidence(id string, fs *models.FilesystemContext) string {
	if readmeContentChecks[id] && fs.ReadmeUnread {
		return "README could not be read"
	}
	dirs := fsEvidence[id]
	for _, d := range fs.UnreadDirs {
		if nameMatched[id] || slices.Contains(dirs, d) {
			if d == "" {
				return "repository root listing could not be read"
			}
			return d + "/ listing could not be read"
		}
	}
	return ""
}

// base provides ID()/Layer() for the concrete checks via embedding.
type base struct {
	id    string
	layer Layer
}

func (b base) ID() string   { return b.id }
func (b base) Layer() Layer { return b.layer }

func (b base) pass() models.CheckResult {
	return models.CheckResult{CheckID: b.id, Status: models.StatusPass, Severity: "unknown"}
}

func (b base) fail(message string) models.CheckResult {
	return models.CheckResult{CheckID: b.id, Status: models.StatusFail, Severity: "unknown", Message: &message}
}

// unobservable reports that a check applies but its evidence could not be read.
// It is deliberately not StatusSkip: a skip is excluded from scoring entirely,
// whereas an unobserved check must reduce coverage so the gap stays visible.
func unobservable(id, message string) models.CheckResult {
	return models.CheckResult{CheckID: id, Status: models.StatusUnknown, Severity: "unknown", Message: &message}
}
