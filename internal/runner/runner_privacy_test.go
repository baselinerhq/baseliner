package runner

import (
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
		// Local / non-GitHub source -> omitted (treated as public downstream).
		{Type: "local", Slug: "local/x", Path: "/tmp/x"},
	}
	vis := repoVisibility(sources)
	want := map[string]string{
		"o/pub":      "public",
		"o/priv":     "private",
		"o/intern":   "internal",
		"o/fallback": "private",
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
