package runner

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/go-github/v68/github"

	"github.com/baselinerhq/baseliner/internal/config"
	"github.com/baselinerhq/baseliner/internal/privacy"
	"github.com/baselinerhq/baseliner/internal/source"
)

func TestRepoVisibility(t *testing.T) {
	sources := []source.Repo{
		{Type: "github", Slug: "o/pub", GitHubRepo: &github.Repository{Visibility: github.Ptr("public")}},
		{Type: "github", Slug: "o/priv", GitHubRepo: &github.Repository{Visibility: github.Ptr("private")}},
		{Type: "github", Slug: "o/intern", GitHubRepo: &github.Repository{Visibility: github.Ptr("internal")}},
		// Visibility unset -> fall back to the Private bool.
		{Type: "github", Slug: "o/fallback", GitHubRepo: &github.Repository{Private: github.Ptr(true)}},
		// private: true wins over a visibility string that disagrees, as it
		// does in discovery's logName.
		{Type: "github", Slug: "o/contradicts", GitHubRepo: &github.Repository{Private: github.Ptr(true), Visibility: github.Ptr("public")}},
		// Local / non-GitHub source -> omitted (treated as public downstream).
		{Type: "local", Slug: "local/x", Path: "/tmp/x"},
	}
	vis := repoVisibility(sources)
	want := map[string]string{
		"o/pub":         "public",
		"o/priv":        "private",
		"o/intern":      "internal",
		"o/fallback":    "private",
		"o/contradicts": "private",
	}
	if len(vis) != len(want) {
		t.Fatalf("got %d entries, want %d: %v", len(vis), len(want), vis)
	}
	for slug, w := range want {
		if vis[slug] != w {
			t.Errorf("vis[%q] = %q, want %q", slug, vis[slug], w)
		}
	}
	if _, ok := vis["local/x"]; ok {
		t.Error("local source must not appear in the visibility map")
	}
}

// In a public context nothing written to stderr — a log record or the runner's
// own messages, e.g. the --fail-under list — may name a private repo. With the
// guard off the same writes keep the name, which shows the check can see a leak.
func TestGuardStderr(t *testing.T) {
	sources := []source.Repo{
		{Type: "github", Slug: "o/priv", GitHubRepo: &github.Repository{Visibility: github.Ptr("private")}},
	}
	for _, c := range []struct {
		name     string
		public   bool
		wantName bool
	}{
		{"public context redacts", true, false},
		{"private context keeps names", false, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			var logs, errb bytes.Buffer
			prev := slog.Default()
			testLogger := slog.New(slog.NewTextHandler(&logs, nil))
			slog.SetDefault(testLogger)
			defer slog.SetDefault(prev)

			cfg := &config.Config{Privacy: &config.PrivacyConfig{PublicContext: c.public}}
			stderr, restore := guardStderr(&errb, sources, cfg, Options{})
			slog.Info("created issue", "repo", "o/priv")
			fmt.Fprintf(stderr, "1 repo(s) below --fail-under 0.90: o/priv (0.50)\n")
			restore()

			for sink, out := range map[string]string{"log": logs.String(), "stderr": errb.String()} {
				if got := strings.Contains(out, "o/priv"); got != c.wantName {
					t.Errorf("%s contains o/priv = %v, want %v:\n%s", sink, got, c.wantName, out)
				}
			}
			if slog.Default() != testLogger {
				t.Error("restore() must put the previous default logger back")
			}
		})
	}
}

// The slug spells the owner as the config does, but collector and issue
// errors quote API URLs built from the repo's owner login. If GitHub ever
// returns a repo whose login differs from the config's spelling by more than
// case, the guard must still match the URL's form.
func TestGuardStderrRedactsGitHubSpelling(t *testing.T) {
	sources := []source.Repo{{Type: "github", Slug: "old-acme/priv", GitHubRepo: &github.Repository{
		Name:       github.Ptr("priv"),
		FullName:   github.Ptr("acme/priv"),
		Owner:      &github.User{Login: github.Ptr("acme")},
		Visibility: github.Ptr("private"),
	}}}
	var logs, errb bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(prev)

	cfg := &config.Config{Privacy: &config.PrivacyConfig{PublicContext: true}}
	_, restore := guardStderr(&errb, sources, cfg, Options{})
	slog.Warn("failed to list branches", "err", errors.New("GET https://api.github.com/repos/acme/priv/branches: 500"))
	restore()
	if strings.Contains(logs.String(), "priv/branches") {
		t.Errorf("the GitHub-spelled URL was not redacted:\n%s", logs.String())
	}
}

func TestPrivacyOptionsResolution(t *testing.T) {
	tru, fls := true, false
	cases := []struct {
		name       string
		cfg        *config.PrivacyConfig
		flag       *bool
		wantPublic bool
		wantMode   privacy.Mode
	}{
		{"no config", nil, nil, false, privacy.ModeRedact},
		{"config public+exclude", &config.PrivacyConfig{PublicContext: true, PrivateRepos: "exclude"}, nil, true, privacy.ModeExclude},
		{"config empty mode defaults redact", &config.PrivacyConfig{PublicContext: true}, nil, true, privacy.ModeRedact},
		{"flag overrides config true->false", &config.PrivacyConfig{PublicContext: true}, &fls, false, privacy.ModeRedact},
		{"flag overrides config false->true", &config.PrivacyConfig{PublicContext: false}, &tru, true, privacy.ModeRedact},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := privacyOptions(&config.Config{Privacy: c.cfg}, Options{PublicContext: c.flag})
			if got.PublicContext != c.wantPublic || got.Mode != c.wantMode {
				t.Errorf("got %+v, want public=%v mode=%v", got, c.wantPublic, c.wantMode)
			}
		})
	}
}
