package privacy

import (
	"strings"
	"testing"
	"time"

	"github.com/baselinerhq/baseliner/internal/models"
)

func sp(s string) *string { return &s }

// fixture builds a run mixing public, private, internal, local, and a
// collection-error private repo, plus the matching visibility map.
func fixture() (models.RunResult, map[string]string) {
	ts := time.Date(2026, 6, 17, 4, 0, 0, 0, time.UTC)
	run := models.RunResult{
		RunID:      "run-1",
		Timestamp:  ts,
		TotalRepos: 5,
		Passed:     3,
		Failed:     2,
		Repos: []models.RepoResult{
			{Slug: "acme/pub", Timestamp: ts, Score: 1.0, Results: []models.CheckResult{
				{CheckID: "readme_exists", Status: models.StatusPass, Severity: models.SeverityCritical},
			}},
			{Slug: "acme/priv", Timestamp: ts, Score: 0.4, Results: []models.CheckResult{
				{CheckID: "license_exists", Status: models.StatusFail, Severity: models.SeverityHigh, Message: sp("No LICENSE in acme/priv")},
			}},
			{Slug: "acme/intern", Timestamp: ts, Score: 1.0, Results: []models.CheckResult{
				{CheckID: "readme_exists", Status: models.StatusPass, Severity: models.SeverityCritical},
			}},
			{Slug: "local/repo", Timestamp: ts, Score: 1.0, Results: []models.CheckResult{
				{CheckID: "readme_exists", Status: models.StatusPass, Severity: models.SeverityCritical},
			}},
			// collection-error private repo: error message embeds a private path.
			{Slug: "acme/secret", Timestamp: ts, Score: 0, Results: []models.CheckResult{
				{CheckID: "collection_error", Status: models.StatusError, Severity: models.SeverityCritical, Message: sp("clone failed for acme/secret")},
			}},
		},
	}
	vis := map[string]string{
		"acme/pub":    "public",
		"acme/priv":   "private",
		"acme/intern": "internal",
		"acme/secret": "private",
		// local/repo intentionally absent -> treated as public.
	}
	return run, vis
}

func TestApplyInactive(t *testing.T) {
	run, vis := fixture()
	cases := []Options{
		{PublicContext: false, Mode: ModeRedact},
		{PublicContext: false, Mode: ModeExclude},
		{PublicContext: true, Mode: ModeAllow},
		{PublicContext: true, Mode: ""},
	}
	for _, o := range cases {
		got, err := Apply(run, vis, o)
		if err != nil {
			t.Fatalf("%+v: unexpected error %v", o, err)
		}
		if got.Privacy != nil {
			t.Errorf("%+v: expected no privacy note, got %+v", o, got.Privacy)
		}
		if len(got.Repos) != 5 || got.Repos[1].Slug != "acme/priv" {
			t.Errorf("%+v: expected repos disclosed unchanged", o)
		}
	}
}

func TestApplyRedact(t *testing.T) {
	run, vis := fixture()
	got, err := Apply(run, vis, Options{PublicContext: true, Mode: ModeRedact})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Slugs masked in order of appearance among protected repos. "internal"
	// (acme/intern) is protected like private, so it is masked too.
	want := []string{"acme/pub", "private/1", "private/2", "local/repo", "private/3"}
	for i, w := range want {
		if got.Repos[i].Slug != w {
			t.Errorf("repo[%d].Slug = %q, want %q", i, got.Repos[i].Slug, w)
		}
	}
	// Aggregate counts unchanged (repos still counted).
	if got.TotalRepos != 5 || got.Passed != 3 || got.Failed != 2 {
		t.Errorf("counts changed: %d/%d/%d", got.TotalRepos, got.Passed, got.Failed)
	}
	// Private repos keep score + statuses but have nulled messages.
	priv := got.Repos[1]
	if float64(priv.Score) != 0.4 || priv.Results[0].Status != models.StatusFail {
		t.Errorf("redacted repo lost score/status: %+v", priv)
	}
	for _, rr := range []models.RepoResult{got.Repos[1], got.Repos[4]} {
		for _, c := range rr.Results {
			if c.Message != nil {
				t.Errorf("redacted repo %q still carries message %q", rr.Slug, *c.Message)
			}
		}
	}
	// Public/local repos keep their messages and identity.
	if got.Repos[0].Slug != "acme/pub" || got.Repos[3].Slug != "local/repo" {
		t.Error("public/local repos must not be masked")
	}
	if got.Privacy == nil || got.Privacy.Mode != "redact" || got.Privacy.Count != 3 {
		t.Errorf("privacy note = %+v, want redact/3", got.Privacy)
	}
	assertNotMutated(t, run)
}

func TestApplyExclude(t *testing.T) {
	run, vis := fixture()
	got, err := Apply(run, vis, Options{PublicContext: true, Mode: ModeExclude})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Repos) != 2 {
		t.Fatalf("got %d repos, want 2 (pub + local)", len(got.Repos))
	}
	for _, rr := range got.Repos {
		if rr.Slug != "acme/pub" && rr.Slug != "local/repo" {
			t.Errorf("private repo leaked into exclude output: %q", rr.Slug)
		}
	}
	// Counts recomputed for the disclosed subset (pub + local both pass).
	if got.TotalRepos != 2 || got.Passed != 2 || got.Failed != 0 {
		t.Errorf("recomputed counts = %d/%d/%d, want 2/2/0", got.TotalRepos, got.Passed, got.Failed)
	}
	if got.Privacy == nil || got.Privacy.Mode != "exclude" || got.Privacy.Count != 3 {
		t.Errorf("privacy note = %+v, want exclude/3", got.Privacy)
	}
	assertNotMutated(t, run)
}

func TestApplyFail(t *testing.T) {
	run, vis := fixture()
	got, err := Apply(run, vis, Options{PublicContext: true, Mode: ModeFail})
	if err == nil {
		t.Fatal("expected an error in fail mode with private repos present")
	}
	// The error must not name any private repo (it goes to the public log).
	for _, slug := range []string{"acme/priv", "acme/intern", "acme/secret"} {
		if strings.Contains(err.Error(), slug) {
			t.Errorf("fail-mode error leaks private slug %q: %s", slug, err.Error())
		}
	}
	if len(got.Repos) != 0 {
		t.Error("fail mode must return an empty result so nothing is written")
	}
}

func TestApplyNoProtectedRepos(t *testing.T) {
	ts := time.Date(2026, 6, 17, 4, 0, 0, 0, time.UTC)
	run := models.RunResult{
		TotalRepos: 1, Passed: 1,
		Repos: []models.RepoResult{{Slug: "acme/pub", Timestamp: ts, Score: 1.0}},
	}
	vis := map[string]string{"acme/pub": "public"}
	// Even in fail mode, no private repos => no error, unchanged.
	for _, m := range []Mode{ModeRedact, ModeExclude, ModeFail} {
		got, err := Apply(run, vis, Options{PublicContext: true, Mode: m})
		if err != nil {
			t.Fatalf("mode %q: unexpected error %v", m, err)
		}
		if got.Privacy != nil {
			t.Errorf("mode %q: unexpected privacy note %+v", m, got.Privacy)
		}
	}
}

func TestParseMode(t *testing.T) {
	if m, err := ParseMode(""); err != nil || m != DefaultMode {
		t.Errorf("ParseMode(\"\") = %q,%v, want %q,nil", m, err, DefaultMode)
	}
	for _, ok := range []string{"allow", "redact", "exclude", "fail"} {
		if _, err := ParseMode(ok); err != nil {
			t.Errorf("ParseMode(%q) errored: %v", ok, err)
		}
	}
	if _, err := ParseMode("bogus"); err == nil {
		t.Error("ParseMode(\"bogus\") should error")
	}
}

// assertNotMutated checks the original run is untouched after a transform.
func assertNotMutated(t *testing.T, run models.RunResult) {
	t.Helper()
	if run.Repos[1].Slug != "acme/priv" || run.Repos[4].Slug != "acme/secret" {
		t.Error("original run slugs were mutated")
	}
	if run.Repos[1].Results[0].Message == nil || run.Privacy != nil {
		t.Error("original run results/note were mutated")
	}
}
