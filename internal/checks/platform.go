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
			unreadable = append(unreadable, fmt.Sprintf("'%s' (%s)", rs.Name, rs.BypassError))
			continue
		}
		if n := countMode(rs, "exempt"); n > 0 {
			exempt = append(exempt, fmt.Sprintf("ruleset '%s' has %d exempt bypass actor(s)", rs.Name, n))
		}
	}
	switch {
	case len(exempt) > 0:
		return c.fail("Exempt bypass (rules not run, no audit entry): " + strings.Join(exempt, "; "))
	case len(unreadable) > 0:
		return unobservable(c.id, "Bypass actors not readable for: "+strings.Join(unreadable, ", "))
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
			parts = append(parts, fmt.Sprintf("'%s' (%s): %d approval(s), bypass: %s",
				rs.Name, rs.SourceType, rs.RequiredApprovals, bypassSummary(rs)))
		}
		rules = "rulesets: " + strings.Join(parts, "; ")
	}
	return classic + "; " + rules + "."
}

// bypassSummary counts bypass actors by mode. Identities are never shown:
// GitHub reveals them to admins only, and this output may be public.
func bypassSummary(rs models.BranchRuleset) string {
	if rs.BypassState != models.SourcePresent {
		return "unreadable"
	}
	if len(rs.BypassActors) == 0 {
		return "none"
	}
	parts := []string{}
	for _, mode := range []string{"always", "pull_request", "exempt"} {
		if n := countMode(rs, mode); n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, mode))
		}
	}
	if other := len(rs.BypassActors) - countMode(rs, "always") - countMode(rs, "pull_request") - countMode(rs, "exempt"); other > 0 {
		parts = append(parts, fmt.Sprintf("%d other", other))
	}
	return strings.Join(parts, ", ")
}

func countMode(rs models.BranchRuleset, mode string) int {
	n := 0
	for _, a := range rs.BypassActors {
		if a.Mode == mode {
			n++
		}
	}
	return n
}
