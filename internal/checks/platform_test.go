package checks

import (
	"strings"
	"testing"

	"github.com/baselinerhq/baseliner/internal/models"
)

func classic(state models.SourceState, approvals int) models.ClassicProtection {
	return models.ClassicProtection{State: state, RequiredApprovals: approvals}
}

func rules(state models.SourceState, rs ...models.BranchRuleset) models.RulesView {
	return models.RulesView{State: state, Rulesets: rs}
}

func ruleset(approvals int, bypass models.SourceState, actors ...models.BypassActor) models.BranchRuleset {
	return models.BranchRuleset{ID: 1, Name: "main-protection", SourceType: "Repository",
		RequiredApprovals: approvals, BypassState: bypass, BypassActors: actors}
}

func platformRepo(c models.ClassicProtection, r models.RulesView) *models.NormalizedRepository {
	return &models.NormalizedRepository{Platform: &models.PlatformContext{
		DefaultBranch: "main", Classic: c, Rules: r}}
}

func TestDefaultBranchRequiresReview(t *testing.T) {
	present, absent, unreadable := models.SourcePresent, models.SourceAbsent, models.SourceUnreadable
	cases := []struct {
		name string
		repo *models.NormalizedRepository
		want models.CheckStatus
	}{
		{"classic only requires review", platformRepo(classic(present, 1), rules(absent)), models.StatusPass},
		{"ruleset only requires review", platformRepo(classic(absent, 0), rules(present, ruleset(2, present))), models.StatusPass},
		{"neither source requires review", platformRepo(classic(absent, 0), rules(absent)), models.StatusFail},
		{"classic present with zero approvals", platformRepo(classic(present, 0), rules(absent)), models.StatusFail},
		// A source that could not be read can never make the answer "no review".
		{"classic unreadable, rules show none", platformRepo(classic(unreadable, 0), rules(absent)), models.StatusUnknown},
		{"rules unreadable, classic shows none", platformRepo(classic(absent, 0), rules(unreadable)), models.StatusUnknown},
		{"both unreadable (free-plan private repo)", platformRepo(classic(unreadable, 0), rules(unreadable)), models.StatusUnknown},
		// But a readable source that does require review is enough on its own.
		{"classic requires review, rules unreadable", platformRepo(classic(present, 1), rules(unreadable)), models.StatusPass},
		{"ruleset requires review, classic unreadable", platformRepo(classic(unreadable, 0), rules(present, ruleset(1, unreadable))), models.StatusPass},
		{"no platform context", &models.NormalizedRepository{}, models.StatusUnknown},
	}
	c, _ := BuildDefault().Get("default_branch_requires_review")
	for _, tc := range cases {
		if got := Evaluate(c, tc.repo).Status; got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
}

// The message is the evidence: it must name both sources, since neither GitHub
// endpoint reports the other.
func TestDefaultBranchRequiresReviewMessageNamesBothSources(t *testing.T) {
	c, _ := BuildDefault().Get("default_branch_requires_review")
	present := models.SourcePresent
	res := Evaluate(c, platformRepo(classic(present, 1), rules(present, ruleset(2, present))))
	if res.Message == nil {
		t.Fatal("want an evidence message on pass")
	}
	for _, want := range []string{"classic", "main-protection", "2"} {
		if !strings.Contains(*res.Message, want) {
			t.Errorf("message %q does not mention %q", *res.Message, want)
		}
	}
}

func TestNoExemptBypass(t *testing.T) {
	present, absent, unreadable := models.SourcePresent, models.SourceAbsent, models.SourceUnreadable
	exempt := models.BypassActor{Mode: "exempt"}
	always := models.BypassActor{Mode: "always"}
	cases := []struct {
		name string
		repo *models.NormalizedRepository
		want models.CheckStatus
	}{
		{"no rulesets", platformRepo(classic(present, 1), rules(absent)), models.StatusPass},
		{"bypass actors without exempt", platformRepo(classic(absent, 0), rules(present, ruleset(1, present, always))), models.StatusPass},
		{"exempt bypass actor", platformRepo(classic(absent, 0), rules(present, ruleset(1, present, always, exempt))), models.StatusFail},
		// No admin access: GitHub omits bypass_actors. That is not "no bypass".
		{"bypass actors unreadable", platformRepo(classic(absent, 0), rules(present, ruleset(1, unreadable))), models.StatusUnknown},
		{"rules view unreadable", platformRepo(classic(present, 1), rules(unreadable)), models.StatusUnknown},
		// A readable exempt outranks an unreadable sibling: the finding is certain.
		{"exempt beside an unreadable ruleset", platformRepo(classic(absent, 0),
			rules(present, ruleset(1, present, exempt), ruleset(1, unreadable))), models.StatusFail},
	}
	c, _ := BuildDefault().Get("no_exempt_bypass")
	for _, tc := range cases {
		if got := Evaluate(c, tc.repo).Status; got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
}

// Bypass actors are admin-only data in GitHub, and findings can land in public
// places (a public control repo's log, an issue on a public repo). Messages
// carry counts by mode, never who can bypass.
func TestBypassMessagesCountModes(t *testing.T) {
	present := models.SourcePresent
	rs := ruleset(1, present, models.BypassActor{Mode: "always"}, models.BypassActor{Mode: "exempt"})
	repo := platformRepo(classic(models.SourceAbsent, 0), rules(present, rs))
	reg := BuildDefault()

	review, _ := reg.Get("default_branch_requires_review")
	if msg := *Evaluate(review, repo).Message; !strings.Contains(msg, "bypass: 1 always, 1 exempt") {
		t.Errorf("review evidence %q, want per-mode counts", msg)
	}
	bypass, _ := reg.Get("no_exempt_bypass")
	if msg := *Evaluate(bypass, repo).Message; !strings.Contains(msg, "ruleset 'main-protection' has 1 exempt bypass actor(s)") {
		t.Errorf("bypass finding %q, want the ruleset and an exempt count", msg)
	}
}
