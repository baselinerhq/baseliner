package checks

import (
	"fmt"
	"strings"

	"github.com/baselinerhq/baseliner/internal/models"
)

// defaultBranchRequiresReview passes when either GitHub source shows the
// default branch requires at least one approving review. GitHub reports
// classic protection and rulesets from two endpoints that do not reference each
// other, so both are read; the stricter one is what is enforced.
//
// A source that could not be read can never turn the answer into "no review
// required": that needs both sources read and neither requiring review.
// Otherwise the result is unknown, which lowers coverage, not posture.
type defaultBranchRequiresReview struct{ base }

func (c defaultBranchRequiresReview) Eval(r *models.NormalizedRepository) models.CheckResult {
	p := r.Platform
	evidence := protectionEvidence(p)

	approvals := 0
	if p.Classic.State == models.SourcePresent {
		approvals = p.Classic.RequiredApprovals
	}
	if p.Rules.State == models.SourcePresent {
		for _, rs := range p.Rules.Rulesets {
			approvals = max(approvals, rs.RequiredApprovals)
		}
	}

	switch {
	case approvals >= 1:
		res := c.pass()
		msg := fmt.Sprintf("Default branch '%s' requires %d approving review(s). %s",
			p.DefaultBranch, approvals, evidence)
		res.Message = &msg
		return res
	case p.Classic.State == models.SourceUnreadable || p.Rules.State == models.SourceUnreadable:
		return unobservable(c.id, fmt.Sprintf(
			"Cannot establish whether default branch '%s' requires review. %s", p.DefaultBranch, evidence))
	default:
		return c.fail(fmt.Sprintf("Default branch '%s' does not require an approving review. %s",
			p.DefaultBranch, evidence))
	}
}

// noExemptBypass fails when a ruleset applying to the default branch has a
// bypass actor in exempt mode: its rules are not run for that actor and, per
// GitHub's API spec, no bypass audit entry is created. The rules view never
// shows bypass actors, so this reads each ruleset object; without admin access
// GitHub omits bypass_actors, which is unknown, not "no bypass".
type noExemptBypass struct{ base }

func (c noExemptBypass) Eval(r *models.NormalizedRepository) models.CheckResult {
	p := r.Platform
	switch p.Rules.State {
	case models.SourceAbsent:
		return c.pass()
	case models.SourceUnreadable:
		return unobservable(c.id, fmt.Sprintf(
			"Cannot read the rules for default branch '%s': %s", p.DefaultBranch, p.Rules.Error))
	}

	var exempt, unreadable []string
	for _, rs := range p.Rules.Rulesets {
		if rs.BypassState != models.SourcePresent {
			unreadable = append(unreadable, fmt.Sprintf("'%s'", rs.Name))
			continue
		}
		for _, a := range rs.BypassActors {
			if a.Mode == "exempt" {
				exempt = append(exempt, fmt.Sprintf("'%s' exempts %s", rs.Name, actorLabel(a)))
			}
		}
	}
	switch {
	case len(exempt) > 0:
		return c.fail("Exempt bypass (rules not run, no audit entry): " + strings.Join(exempt, "; "))
	case len(unreadable) > 0:
		return unobservable(c.id,
			"Bypass actors not readable (needs admin access) for ruleset(s): "+strings.Join(unreadable, ", "))
	default:
		return c.pass()
	}
}

// protectionEvidence summarises both sources, so a finding shows what was read.
func protectionEvidence(p *models.PlatformContext) string {
	var classic string
	switch p.Classic.State {
	case models.SourcePresent:
		classic = fmt.Sprintf("classic protection: %d approval(s), enforce_admins=%t",
			p.Classic.RequiredApprovals, p.Classic.EnforceAdmins)
	case models.SourceAbsent:
		classic = "classic protection: none"
	default:
		classic = "classic protection: unreadable (" + p.Classic.Error + ")"
	}

	var rules string
	switch p.Rules.State {
	case models.SourceAbsent:
		rules = "rulesets: none"
	case models.SourceUnreadable:
		rules = "rulesets: unreadable (" + p.Rules.Error + ")"
	default:
		parts := make([]string, 0, len(p.Rules.Rulesets))
		for _, rs := range p.Rules.Rulesets {
			parts = append(parts, fmt.Sprintf("'%s' (%s): %d approval(s), bypass %s",
				rs.Name, rs.SourceType, rs.RequiredApprovals, bypassSummary(rs)))
		}
		rules = "rulesets: " + strings.Join(parts, "; ")
	}
	return classic + "; " + rules + "."
}

func bypassSummary(rs models.BranchRuleset) string {
	if rs.BypassState != models.SourcePresent {
		return "unreadable"
	}
	if len(rs.BypassActors) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(rs.BypassActors))
	for _, a := range rs.BypassActors {
		parts = append(parts, actorLabel(a)+":"+a.Mode)
	}
	return strings.Join(parts, ",")
}

func actorLabel(a models.BypassActor) string {
	if a.ActorID == 0 {
		return a.ActorType
	}
	return fmt.Sprintf("%s %d", a.ActorType, a.ActorID)
}
