package collectors

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/baselinerhq/baseliner/internal/gitlab"
	"github.com/baselinerhq/baseliner/internal/models"
)

// maxProtectionPages bounds the protected-branch and approval-rule listings
// (100 per page).
const maxProtectionPages = 10

// collectPlatform reads what protects the default branch of a GitLab
// project: the protected-branch rules matching it and the approval rules
// applying to it. Only a positive answer counts as absent; every other
// failure is unreadable.
func (c GitLabAPI) collectPlatform(ctx context.Context, projectID int64, branch string) *models.PlatformContext {
	g := &models.GitLabProtection{}
	rules, complete, err := c.Client.ProtectedBranches(ctx, projectID, maxProtectionPages)
	var matching []gitlab.ProtectedBranch
	switch {
	case err != nil:
		c.observe(err)
		g.Protected, g.ProtectedError = models.SourceUnreadable, err.Error()
	case !complete:
		g.Protected, g.ProtectedError = models.SourceUnreadable, fmt.Sprintf("more than %d pages of protected branches", maxProtectionPages)
	default:
		for _, r := range rules {
			if branchMatches(r.Name, branch) {
				matching = append(matching, r)
			}
		}
		g.Protected = models.SourceAbsent
		if len(matching) > 0 {
			g.Protected = models.SourcePresent
			g.Push = pushSummary(matching)
			g.AllowForcePush = slices.ContainsFunc(matching, func(r gitlab.ProtectedBranch) bool { return r.AllowForcePush })
		}
	}

	approvals, complete, err := c.Client.ApprovalRules(ctx, projectID, maxProtectionPages)
	var ge *gitlab.Error
	switch {
	case errors.As(err, &ge) && ge.Status == http.StatusNotFound && ge.NoRoute:
		// The instance has no approval rules endpoint: GitLab below Premium,
		// where review cannot be required.
		g.Approvals = models.SourceAbsent
	case err != nil:
		c.observe(err)
		g.Approvals, g.ApprovalsError = models.SourceUnreadable, err.Error()
	case !complete:
		g.Approvals, g.ApprovalsError = models.SourceUnreadable, fmt.Sprintf("more than %d pages of approval rules", maxProtectionPages)
	default:
		g.Approvals = models.SourcePresent
		settings, err := c.Client.ApprovalSettings(ctx, projectID)
		if err != nil {
			c.observe(err)
			g.Settings, g.SettingsError = models.SourceUnreadable, err.Error()
		} else {
			g.Settings = models.SourcePresent
			g.RulesEditable = !settings.DisableOverriding
			g.AuthorCanApprove = settings.AuthorCanApprove
		}
		uncountable := false
		for _, a := range approvals {
			if !appliesTo(a, branch, len(matching) > 0) {
				continue
			}
			g.RequiredApprovals = max(g.RequiredApprovals, a.ApprovalsRequired)
			if g.Settings != models.SourcePresent {
				continue
			}
			n, ok := fromOthers(a, settings)
			uncountable = uncountable || !ok
			g.ReviewApprovals = max(g.ReviewApprovals, n)
		}
		if g.ReviewApprovals == 0 && uncountable {
			g.Approvals, g.ApprovalsError = models.SourceUnreadable, "the approvers of an approval rule could not be counted"
		}
	}
	return &models.PlatformContext{DefaultBranch: branch, GitLab: g}
}

// fromOthers returns how many approvals a rule needs from someone other
// than the merge request's author, as GitLab counts them, and false when
// that cannot be told. It assumes the worst author: one of the rule's own
// approvers. GitLab treats a rule, but an any-approver one, with fewer
// approvers than it requires as approved, needing none; an author who may
// not approve their own merge request leaves the rule's approvers, which
// can tip it there. Approvers hidden in groups the token cannot see, or
// left out of the listing, cannot be counted, nor can the effect of
// barring committers from approving, which removes an unknown number.
func fromOthers(a gitlab.ApprovalRule, s gitlab.ApprovalSettings) (int, bool) {
	r := a.ApprovalsRequired
	if a.RuleType == "any_approver" {
		if s.AuthorCanApprove {
			r--
		}
		return max(r, 0), true
	}
	if a.ContainsHiddenGroups || a.EligibleApprovers == nil || s.DisableCommitters {
		return 0, false
	}
	n := len(a.EligibleApprovers)
	switch {
	case s.AuthorCanApprove && r <= n:
		return max(r-1, 0), true
	case !s.AuthorCanApprove && r <= n-1:
		return r, true
	}
	return 0, true
}

// appliesTo reports whether an approval rule requires approvals on merge
// requests into branch. Code owner and security report rules apply only to
// some changes, so they never count. Approval policies (Ultimate) are not
// among the rules GitLab lists here at all.
func appliesTo(a gitlab.ApprovalRule, branch string, protected bool) bool {
	if a.RuleType != "regular" && a.RuleType != "any_approver" {
		return false
	}
	if a.AppliesToAllProtectedBranches {
		return protected
	}
	if len(a.ProtectedBranches) == 0 {
		return true
	}
	for _, pb := range a.ProtectedBranches {
		if branchMatches(pb.Name, branch) {
			return true
		}
	}
	return false
}

// branchMatches reports whether a protected-branch name, in which * matches
// any characters, matches branch.
func branchMatches(pattern, branch string) bool {
	if !strings.Contains(pattern, "*") {
		return pattern == branch
	}
	parts := strings.Split(pattern, "*")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	return regexp.MustCompile("^" + strings.Join(parts, ".*") + "$").MatchString(branch)
}

// pushSummary says who may push to the branch directly under the matching
// rules. GitLab applies the most permissive rule, so every grant counts.
func pushSummary(rules []gitlab.ProtectedBranch) string {
	roles := map[int]string{0: "no one", 30: "developers and maintainers", 40: "maintainers", 60: "administrators"}
	var parts []string
	add := func(s string) {
		if !slices.Contains(parts, s) {
			parts = append(parts, s)
		}
	}
	for _, r := range rules {
		for _, l := range r.PushAccessLevels {
			switch {
			case l.UserID != nil:
				add("specific users")
			case l.GroupID != nil:
				add("specific groups")
			case l.DeployKeyID != nil:
				add("deploy keys")
			case roles[l.AccessLevel] != "":
				add(roles[l.AccessLevel])
			default:
				add(fmt.Sprintf("access level %d", l.AccessLevel))
			}
		}
	}
	if len(parts) > 1 {
		parts = slices.DeleteFunc(parts, func(s string) bool { return s == "no one" })
	}
	if len(parts) == 0 {
		return "no one"
	}
	return strings.Join(parts, ", ")
}
