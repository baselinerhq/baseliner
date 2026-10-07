package collectors

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/go-github/v68/github"

	"github.com/baselinerhq/baseliner/internal/models"
	"github.com/baselinerhq/baseliner/internal/source"
)

func ghSource(repo *github.Repository) source.Repo {
	return source.Repo{Type: "github", Slug: "o/r", GitHubRepo: repo}
}

// platformMux serves only the three platform endpoints; file listings 404,
// which the fs collector already treats as absent.
func platformMux(classic, rules http.HandlerFunc, rulesets map[string]string) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/o/r/branches/main/protection", classic)
	mux.HandleFunc("GET /repos/o/r/rules/branches/main", rules)
	for id, body := range rulesets {
		body := body
		mux.HandleFunc("GET /repos/o/r/rulesets/"+id, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(body))
		})
	}
	mux.HandleFunc("GET /", http.NotFound)
	return mux
}

func respond(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func collectPlatform(t *testing.T, mux *http.ServeMux) *models.PlatformContext {
	t.Helper()
	c := NewGitHubAPI(fakeGitHubClient(t, mux))
	c.Platform = true
	repo := &github.Repository{Owner: &github.User{Login: github.Ptr("o")}, Name: github.Ptr("r"),
		DefaultBranch: github.Ptr("main")}
	got := c.Collect(context.Background(), ghSource(repo))
	if got.Platform == nil {
		t.Fatal("Platform context missing with Platform enabled")
	}
	return got.Platform
}

const prRule = `{"type":"pull_request","ruleset_source_type":"Repository","ruleset_source":"o/r","ruleset_id":7,` +
	`"parameters":{"required_approving_review_count":2}}`

func TestPlatformClassicPresentNoRulesets(t *testing.T) {
	p := collectPlatform(t, platformMux(
		respond(200, `{"required_pull_request_reviews":{"required_approving_review_count":1},"enforce_admins":{"enabled":true}}`),
		respond(200, `[]`), nil))
	if p.Classic.State != models.SourcePresent || p.Classic.RequiredApprovals != 1 || !p.Classic.EnforceAdmins {
		t.Errorf("classic = %+v", p.Classic)
	}
	if p.Rules.State != models.SourceAbsent {
		t.Errorf("rules state = %s, want absent", p.Rules.State)
	}
}

func TestPlatformRulesetWithExemptBypass(t *testing.T) {
	p := collectPlatform(t, platformMux(
		respond(404, `{"message":"Branch not protected"}`),
		respond(200, `[{"type":"deletion","ruleset_id":7},`+prRule+`]`),
		map[string]string{"7": `{"id":7,"name":"main-protection","source_type":"Repository","enforcement":"active",` +
			`"bypass_actors":[{"actor_id":5,"actor_type":"RepositoryRole","bypass_mode":"exempt"}]}`}))
	if p.Classic.State != models.SourceAbsent {
		t.Errorf("classic state = %s, want absent (\"Branch not protected\")", p.Classic.State)
	}
	if p.Rules.State != models.SourcePresent || len(p.Rules.Rulesets) != 1 {
		t.Fatalf("rules = %+v, want one ruleset (rules grouped by ruleset_id)", p.Rules)
	}
	rs := p.Rules.Rulesets[0]
	if rs.Name != "main-protection" || rs.RequiredApprovals != 2 || rs.BypassState != models.SourcePresent ||
		len(rs.BypassActors) != 1 || rs.BypassActors[0].Mode != "exempt" {
		t.Errorf("ruleset = %+v", rs)
	}
}

// A 404 that is not "Branch not protected" (e.g. a token without access) and a
// plan-gated 403 are both unreadable, never absent.
func TestPlatformUnreadableSources(t *testing.T) {
	p := collectPlatform(t, platformMux(
		respond(404, `{"message":"Not Found"}`),
		respond(403, `{"message":"Upgrade to GitHub Pro or make this repository public to enable this feature."}`),
		nil))
	if p.Classic.State != models.SourceUnreadable {
		t.Errorf("classic state = %s, want unreadable for a generic 404", p.Classic.State)
	}
	if p.Rules.State != models.SourceUnreadable {
		t.Errorf("rules state = %s, want unreadable for 403", p.Rules.State)
	}
}

// Without admin access GitHub returns the ruleset but omits bypass_actors.
func TestPlatformBypassActorsOmitted(t *testing.T) {
	p := collectPlatform(t, platformMux(
		respond(404, `{"message":"Branch not protected"}`),
		respond(200, `[`+prRule+`]`),
		map[string]string{"7": `{"id":7,"name":"main-protection","source_type":"Repository","enforcement":"active"}`}))
	if got := p.Rules.Rulesets[0].BypassState; got != models.SourceUnreadable {
		t.Errorf("bypass state = %s, want unreadable when bypass_actors is omitted", got)
	}
}

func TestPlatformNotCollectedByDefault(t *testing.T) {
	c := NewGitHubAPI(fakeGitHubClient(t, platformMux(respond(500, ""), respond(500, ""), nil)))
	repo := &github.Repository{Owner: &github.User{Login: github.Ptr("o")}, Name: github.Ptr("r"),
		DefaultBranch: github.Ptr("main")}
	if got := c.Collect(context.Background(), ghSource(repo)); got.Platform != nil {
		t.Errorf("Platform = %+v, want nil when no platform check is enabled", got.Platform)
	}
}
