package config

import (
	"strings"
	"testing"
)

func TestGitLabScopeDefaults(t *testing.T) {
	cfg, err := Load(write(t, "scope:\n  gitlab:\n    group: /acme/platform/\n"))
	if err != nil {
		t.Fatal(err)
	}
	gl := cfg.Scope.GitLab
	if gl.Group != "acme/platform" || gl.BaseURL != "https://gitlab.com" || gl.TokenEnv != "GITLAB_TOKEN" || gl.IncludeArchived {
		t.Errorf("scope.gitlab = %+v", gl)
	}
}

// base_url is the instance root: an API path or trailing slash is dropped, so
// requests are built the same way however it is written.
func TestGitLabBaseURLNormalised(t *testing.T) {
	for in, want := range map[string]string{
		"https://gitlab.example.com":              "https://gitlab.example.com",
		"https://gitlab.example.com/":             "https://gitlab.example.com",
		"https://gitlab.example.com/api/v4":       "https://gitlab.example.com",
		"https://gitlab.example.com/api/v4/":      "https://gitlab.example.com",
		"https://example.com/gitlab/api/v4":       "https://example.com/gitlab",
		"http://gitlab.internal:8080":             "http://gitlab.internal:8080",
		" https://gitlab.example.com/sub/gitlab ": "https://gitlab.example.com/sub/gitlab",
	} {
		cfg, err := Load(write(t, "scope:\n  gitlab:\n    group: acme\n    base_url: \""+in+"\"\n"))
		if err != nil {
			t.Errorf("base_url %q: %v", in, err)
			continue
		}
		if got := cfg.Scope.GitLab.BaseURL; got != want {
			t.Errorf("base_url %q = %q, want %q", in, got, want)
		}
	}
}

func TestGitLabScopeRejects(t *testing.T) {
	for name, body := range map[string]string{
		"no group":      "scope:\n  gitlab:\n    base_url: https://gitlab.com\n",
		"blank group":   "scope:\n  gitlab:\n    group: \" / \"\n",
		"relative url":  "scope:\n  gitlab:\n    group: acme\n    base_url: gitlab.example.com\n",
		"other scheme":  "scope:\n  gitlab:\n    group: acme\n    base_url: ftp://gitlab.example.com\n",
		"query":         "scope:\n  gitlab:\n    group: acme\n    base_url: https://gitlab.example.com/?x=1\n",
		"credentials":   "scope:\n  gitlab:\n    group: acme\n    base_url: https://user:pw@gitlab.example.com\n",
		"unknown key":   "scope:\n  gitlab:\n    group: acme\n    token: abc\n",
		"no host":       "scope:\n  gitlab:\n    group: acme\n    base_url: https:///api/v4\n",
		"fragment only": "scope:\n  gitlab:\n    group: acme\n    base_url: https://gitlab.example.com#x\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(write(t, body)); err == nil {
				t.Error("want an error")
			} else if strings.Contains(err.Error(), "pw") {
				t.Errorf("the error repeats a credential: %v", err)
			}
		})
	}
}

// GitHub and GitLab can be scanned in one run, with local paths too.
func TestGitHubAndGitLabTogether(t *testing.T) {
	cfg, err := Load(write(t, "scope:\n  github:\n    type: org\n    name: acme\n  gitlab:\n    group: acme\n  local:\n    paths: [.]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Scope.GitHub == nil || cfg.Scope.GitLab == nil || cfg.Scope.Local == nil {
		t.Errorf("scope = %+v", cfg.Scope)
	}
}
