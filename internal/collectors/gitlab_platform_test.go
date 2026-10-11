package collectors

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/baselinerhq/baseliner/internal/gitlab"
	"github.com/baselinerhq/baseliner/internal/models"
	"github.com/baselinerhq/baseliner/internal/source"
)

// The platform layer on GitLab: protected-branch rules matching the default
// branch (wildcards included) and approval rules applying to it. Only a
// positive answer is absence: no matching rule, or no approval rules
// endpoint at all (below Premium, as on CE); any other failure is
// unreadable. Who may push is summarised without identities.
func TestGitLabCollectPlatform(t *testing.T) {
	const protectedMain = `[{"name":"main","push_access_levels":[{"access_level":0}],"merge_access_levels":[{"access_level":40}],"allow_force_push":false}]`
	const locked = `{"disable_overriding_approvers_per_merge_request":true,"merge_requests_author_approval":false}`
	const two = `"eligible_approvers":[{"id":1},{"id":2}]`
	read := func(g models.GitLabProtection) models.GitLabProtection {
		g.Settings = models.SourcePresent
		return g
	}
	for _, c := range []struct {
		name                           string
		protected, approvals, settings any
		want                           models.GitLabProtection
	}{
		{"protected, rule on all protected branches", protectedMain,
			`[{"name":"r","rule_type":"regular","approvals_required":2,"applies_to_all_protected_branches":true,` + two + `}]`, locked,
			read(models.GitLabProtection{Protected: models.SourcePresent, Push: "no one", Approvals: models.SourcePresent, RequiredApprovals: 2})},
		{"wildcard, users and a deploy key", `[{"name":"ma*","push_access_levels":[{"access_level":40},{"access_level":40,"user_id":7},{"access_level":0,"deploy_key_id":3}],"allow_force_push":true},{"name":"other","push_access_levels":[{"access_level":30}]}]`,
			`[{"name":"r","rule_type":"any_approver","approvals_required":1,"protected_branches":[]}]`, locked,
			read(models.GitLabProtection{Protected: models.SourcePresent, Push: "maintainers, specific users, deploy keys", AllowForcePush: true, Approvals: models.SourcePresent, RequiredApprovals: 1})},
		{"fewer approvers than required, as GitLab counts it", protectedMain,
			`[{"name":"r","rule_type":"regular","approvals_required":3,` + two + `}]`, locked,
			read(models.GitLabProtection{Protected: models.SourcePresent, Push: "no one", Approvals: models.SourcePresent, RequiredApprovals: 2})},
		{"no approvers: GitLab treats the rule as approved", protectedMain,
			`[{"name":"r","rule_type":"regular","approvals_required":1,"eligible_approvers":[]}]`, locked,
			read(models.GitLabProtection{Protected: models.SourcePresent, Push: "no one", Approvals: models.SourcePresent})},
		{"approvers in hidden groups", protectedMain,
			`[{"name":"r","rule_type":"regular","approvals_required":1,"eligible_approvers":[],"contains_hidden_groups":true}]`, locked,
			read(models.GitLabProtection{Protected: models.SourcePresent, Push: "no one", Approvals: models.SourceUnreadable})},
		{"approvers left out", protectedMain,
			`[{"name":"r","rule_type":"regular","approvals_required":1}]`, locked,
			read(models.GitLabProtection{Protected: models.SourcePresent, Push: "no one", Approvals: models.SourceUnreadable})},
		{"a countable rule settles an uncountable one", protectedMain,
			`[{"name":"r","rule_type":"regular","approvals_required":1,"contains_hidden_groups":true},{"name":"s","rule_type":"any_approver","approvals_required":1}]`, locked,
			read(models.GitLabProtection{Protected: models.SourcePresent, Push: "no one", Approvals: models.SourcePresent, RequiredApprovals: 1})},
		{"rules that do not count, one an approval policy", protectedMain,
			`[{"name":"co","rule_type":"code_owner","approvals_required":3,` + two + `},{"name":"sec","rule_type":"report_approver","approvals_required":3,` + two + `},{"name":"rel","rule_type":"regular","approvals_required":3,"protected_branches":[{"name":"release/*"}],` + two + `}]`, locked,
			read(models.GitLabProtection{Protected: models.SourcePresent, Push: "no one", Approvals: models.SourcePresent, PolicyRules: true})},
		{"most approvals of several rules", protectedMain,
			`[{"name":"a","rule_type":"any_approver","approvals_required":3},{"name":"b","rule_type":"any_approver","approvals_required":1}]`, locked,
			read(models.GitLabProtection{Protected: models.SourcePresent, Push: "no one", Approvals: models.SourcePresent, RequiredApprovals: 3})},
		{"rules editable, authors approve", protectedMain,
			`[{"name":"r","rule_type":"any_approver","approvals_required":1}]`, `{"disable_overriding_approvers_per_merge_request":false,"merge_requests_author_approval":true}`,
			read(models.GitLabProtection{Protected: models.SourcePresent, Push: "no one", Approvals: models.SourcePresent, RequiredApprovals: 1, RulesEditable: true, AuthorCanApprove: true})},
		{"settings unreadable", protectedMain,
			`[{"name":"r","rule_type":"any_approver","approvals_required":1}]`, http.StatusForbidden,
			models.GitLabProtection{Protected: models.SourcePresent, Push: "no one", Approvals: models.SourcePresent, RequiredApprovals: 1, Settings: models.SourceUnreadable}},
		{"rule on all protected branches, branch unprotected", `[{"name":"release/*"}]`,
			`[{"name":"r","rule_type":"any_approver","approvals_required":1,"applies_to_all_protected_branches":true}]`, locked,
			read(models.GitLabProtection{Protected: models.SourceAbsent, Approvals: models.SourcePresent})},
		{"no approval rules on this instance", protectedMain, noRoute{}, noRoute{},
			models.GitLabProtection{Protected: models.SourcePresent, Push: "no one", Approvals: models.SourceAbsent}},
		{"approval rules refused", protectedMain, http.StatusForbidden, locked,
			models.GitLabProtection{Protected: models.SourcePresent, Push: "no one", Approvals: models.SourceUnreadable}},
		{"approval rules missing as a resource", protectedMain, nil, locked,
			models.GitLabProtection{Protected: models.SourcePresent, Push: "no one", Approvals: models.SourceUnreadable}},
		{"protected branches past the page cap", endless{}, `[]`, locked,
			read(models.GitLabProtection{Protected: models.SourceUnreadable, Approvals: models.SourcePresent})},
		{"protected branches unreadable", http.StatusInternalServerError, `[]`, locked,
			read(models.GitLabProtection{Protected: models.SourceUnreadable, Approvals: models.SourcePresent})},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := &glFake{trees: map[string]any{"": `[]`}, protected: c.protected, approvals: c.approvals, settings: c.settings}
			srv := httptest.NewServer(http.HandlerFunc(f.handler))
			defer srv.Close()
			client, err := gitlab.New(srv.URL, "tok")
			if err != nil {
				t.Fatal(err)
			}
			col := NewGitLabAPI(client)
			col.Platform = true
			p := project()
			r := col.Collect(context.Background(), source.Repo{Type: "gitlab", Slug: "acme/team/app", ForgeRepo: &p})
			if r.Platform == nil || r.Platform.GitLab == nil {
				t.Fatalf("no GitLab platform context: %+v", r.Platform)
			}
			g := *r.Platform.GitLab
			if (g.ProtectedError != "") != (g.Protected == models.SourceUnreadable) || (g.ApprovalsError != "") != (g.Approvals == models.SourceUnreadable) ||
				(g.SettingsError != "") != (g.Settings == models.SourceUnreadable) {
				t.Errorf("errors do not match states: %+v", g)
			}
			g.ProtectedError, g.ApprovalsError, g.SettingsError = "", "", ""
			if g != c.want {
				t.Errorf("got  %+v\nwant %+v", g, c.want)
			}
			if r.Platform.DefaultBranch != "main" {
				t.Errorf("default branch %q", r.Platform.DefaultBranch)
			}
		})
	}
}

// Without the platform layer, or with no default branch, nothing is read.
func TestGitLabCollectPlatformOff(t *testing.T) {
	f := &glFake{trees: map[string]any{"": `[]`}, protected: http.StatusInternalServerError, approvals: http.StatusInternalServerError}
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	defer srv.Close()
	client, _ := gitlab.New(srv.URL, "tok")
	col := NewGitLabAPI(client)
	p := project()
	if r := col.Collect(context.Background(), source.Repo{Type: "gitlab", Slug: "a/b", ForgeRepo: &p}); r.Platform != nil {
		t.Errorf("platform collected while off: %+v", r.Platform)
	}
	col.Platform = true
	p.DefaultBranch = ""
	if r := col.Collect(context.Background(), source.Repo{Type: "gitlab", Slug: "a/b", ForgeRepo: &p}); r.Platform != nil {
		t.Errorf("platform collected with no default branch: %+v", r.Platform)
	}
}

func TestBranchMatches(t *testing.T) {
	for _, c := range []struct {
		pattern, branch string
		want            bool
	}{
		{"main", "main", true}, {"main", "mainline", false}, {"*", "main", true},
		{"ma*", "main", true}, {"release/*", "release/1.0", true}, {"release/*", "main", false},
		{"*-stable", "1-0-stable", true}, {"a.b", "axb", false}, {"Main", "main", false},
		{"ma*", "xmain", false}, {"*-stable", "1-0-stable-x", false},
	} {
		if got := branchMatches(c.pattern, c.branch); got != c.want {
			t.Errorf("branchMatches(%q, %q) = %v", c.pattern, c.branch, got)
		}
	}
}
