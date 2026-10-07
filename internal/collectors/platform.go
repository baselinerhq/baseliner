package collectors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/google/go-github/v68/github"

	"github.com/baselinerhq/baseliner/internal/models"
)

// collectPlatform reads what protects the default branch from both GitHub
// sources. Neither reports the other: classic protection is invisible to the
// rules endpoint, and rulesets are invisible to the protection endpoint. Only a
// positive answer counts as absent; every other failure is unreadable.
func (c GitHubAPI) collectPlatform(ctx context.Context, owner, name, branch string) *models.PlatformContext {
	return &models.PlatformContext{
		DefaultBranch: branch,
		Classic:       c.classicProtection(ctx, owner, name, branch),
		Rules:         c.branchRules(ctx, owner, name, branch),
	}
}

func (c GitHubAPI) classicProtection(ctx context.Context, owner, name, branch string) models.ClassicProtection {
	p, resp, err := c.Client.Repositories.GetBranchProtection(ctx, owner, name, branch)
	switch {
	case errors.Is(err, github.ErrBranchNotProtected):
		// GitHub's 404 "Branch not protected" — a positive answer. A generic 404
		// (no access, hidden private repo) is not this error.
		return models.ClassicProtection{State: models.SourceAbsent}
	case err != nil:
		return models.ClassicProtection{State: models.SourceUnreadable, Error: describe(resp, err)}
	}
	out := models.ClassicProtection{State: models.SourcePresent}
	if r := p.GetRequiredPullRequestReviews(); r != nil {
		out.RequiredApprovals = r.RequiredApprovingReviewCount
	}
	if a := p.GetEnforceAdmins(); a != nil {
		out.EnforceAdmins = a.Enabled
	}
	return out
}

// branchRule is one entry from the rules endpoint, decoded by hand: go-github
// rejects rule types it does not know, and GitHub adds them faster than client
// libraries do, so one unknown type would otherwise blank the whole view.
type branchRule struct {
	Type              string          `json:"type"`
	RulesetSourceType string          `json:"ruleset_source_type"`
	RulesetID         int64           `json:"ruleset_id"`
	Parameters        json.RawMessage `json:"parameters"`
}

func (c GitHubAPI) branchRules(ctx context.Context, owner, name, branch string) models.RulesView {
	// The endpoint pages (30 per page by default); read every page, or rules
	// past the first would be missed and a check could pass on what it never saw.
	var rules []branchRule
	page := 1
	for page != 0 {
		req, err := c.Client.NewRequest("GET", fmt.Sprintf("repos/%s/%s/rules/branches/%s?per_page=100&page=%d",
			owner, name, url.PathEscape(branch), page), nil)
		if err != nil {
			return models.RulesView{State: models.SourceUnreadable, Error: err.Error()}
		}
		var batch []branchRule
		resp, err := c.Client.Do(ctx, req, &batch)
		if err != nil {
			return models.RulesView{State: models.SourceUnreadable, Error: describe(resp, err)}
		}
		rules = append(rules, batch...)
		page = resp.NextPage
	}
	if len(rules) == 0 {
		return models.RulesView{State: models.SourceAbsent, Rulesets: []models.BranchRuleset{}}
	}

	// One entry per rule; group them by ruleset.
	var order []int64
	byID := map[int64]*models.BranchRuleset{}
	for _, r := range rules {
		rs, seen := byID[r.RulesetID]
		if !seen {
			rs = &models.BranchRuleset{ID: r.RulesetID, SourceType: r.RulesetSourceType}
			byID[r.RulesetID] = rs
			order = append(order, r.RulesetID)
		}
		if r.Type == "pull_request" && len(r.Parameters) > 0 {
			var params github.PullRequestRuleParameters
			if json.Unmarshal(r.Parameters, &params) == nil {
				rs.RequiredApprovals = max(rs.RequiredApprovals, params.RequiredApprovingReviewCount)
			}
		}
	}

	out := models.RulesView{State: models.SourcePresent, Rulesets: make([]models.BranchRuleset, 0, len(order))}
	for _, id := range order {
		rs := byID[id]
		c.rulesetBypass(ctx, owner, name, rs)
		out.Rulesets = append(out.Rulesets, *rs)
	}
	return out
}

// rulesetBypass reads a ruleset's name and the modes of its bypass actors. The
// effective-rules view never includes bypass actors, and the ruleset object
// omits the bypass_actors key (rather than returning an empty list) for callers
// without admin access, so a missing key is unreadable, never "no bypass".
// Actor identities are deliberately not kept (see models.BypassActor).
func (c GitHubAPI) rulesetBypass(ctx context.Context, owner, name string, rs *models.BranchRuleset) {
	rs.BypassState = models.SourceUnreadable
	if rs.Name == "" {
		rs.Name = fmt.Sprintf("ruleset %d", rs.ID)
	}
	if rs.ID == 0 {
		rs.BypassError = "rule carries no ruleset_id"
		return
	}
	req, err := c.Client.NewRequest("GET",
		fmt.Sprintf("repos/%s/%s/rulesets/%d?includes_parents=true", owner, name, rs.ID), nil)
	if err != nil {
		rs.BypassError = err.Error()
		return
	}
	var raw struct {
		Name         string `json:"name"`
		SourceType   string `json:"source_type"`
		BypassActors *[]struct {
			BypassMode string `json:"bypass_mode"`
		} `json:"bypass_actors"`
	}
	if resp, err := c.Client.Do(ctx, req, &raw); err != nil {
		rs.BypassError = describe(resp, err)
		return
	}
	if raw.Name != "" {
		rs.Name = raw.Name
	}
	if raw.SourceType != "" {
		rs.SourceType = raw.SourceType
	}
	if raw.BypassActors == nil {
		rs.BypassError = "bypass actors not returned (needs admin access)"
		return
	}
	rs.BypassState = models.SourcePresent
	rs.BypassActors = make([]models.BypassActor, 0, len(*raw.BypassActors))
	for _, a := range *raw.BypassActors {
		rs.BypassActors = append(rs.BypassActors, models.BypassActor{Mode: a.BypassMode})
	}
}

// describe renders an API failure for a finding message: the HTTP status and
// GitHub's message when there is one.
func describe(resp *github.Response, err error) string {
	var ge *github.ErrorResponse
	if errors.As(err, &ge) && ge.Response != nil {
		return fmt.Sprintf("HTTP %d: %s", ge.Response.StatusCode, ge.Message)
	}
	if resp != nil && resp.Response != nil {
		// A non-API error with a response is usually a decode failure on a 200:
		// keep the error, not just a status that reads like success.
		return fmt.Sprintf("HTTP %d: %v", resp.StatusCode, err)
	}
	return err.Error()
}
