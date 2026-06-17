// Package checks implements the baseliner compliance checks and a registry.
//
// Each check declares a required layer (fs/git/platform). Evaluate applies
// layer-skip — if the required context is absent the check is skipped rather
// than failed — then runs the check's own logic. Severity on a result is left
// as "unknown"; the policy engine overrides it from the policy definition.
package checks

import "github.com/baselinerhq/baseliner/internal/models"

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

// Evaluate applies layer-skip then runs the check.
func Evaluate(c Check, repo *models.NormalizedRepository) models.CheckResult {
	switch c.Layer() {
	case LayerFS:
		if repo.FS == nil {
			return skip(c.ID(), "Filesystem context not available")
		}
	case LayerGit:
		if repo.Git == nil {
			return skip(c.ID(), "Git context not available")
		}
	case LayerPlatform:
		if repo.Platform == nil {
			return skip(c.ID(), "Platform context not available")
		}
	}
	return c.Eval(repo)
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
	return models.CheckResult{CheckID: b.id, Status: models.StatusFail, Severity: "unknown", Message: message}
}

func skip(id, message string) models.CheckResult {
	return models.CheckResult{CheckID: id, Status: models.StatusSkip, Severity: "unknown", Message: message}
}
