package config

import (
	"strings"
	"testing"
)

func TestGiteaScopeDefaults(t *testing.T) {
	cfg, err := Load(write(t, "scope:\n  gitea:\n    type: org\n    name: \" acme \"\n"))
	if err != nil {
		t.Fatal(err)
	}
	gt := cfg.Scope.Gitea
	if gt.Name != "acme" || gt.BaseURL != "https://codeberg.org" || gt.TokenEnv != "GITEA_TOKEN" || gt.IncludeArchived {
		t.Errorf("scope.gitea = %+v", gt)
	}
}

func TestGiteaBaseURLNormalised(t *testing.T) {
	for in, want := range map[string]string{
		"https://git.example.com":         "https://git.example.com",
		"https://git.example.com/api/v1":  "https://git.example.com",
		"https://git.example.com/api/v1/": "https://git.example.com",
		"http://forgejo.internal:3000/":   "http://forgejo.internal:3000",
	} {
		cfg, err := Load(write(t, "scope:\n  gitea:\n    type: user\n    name: someone\n    base_url: \""+in+"\"\n"))
		if err != nil {
			t.Errorf("base_url %q: %v", in, err)
			continue
		}
		if got := cfg.Scope.Gitea.BaseURL; got != want {
			t.Errorf("base_url %q = %q, want %q", in, got, want)
		}
	}
}

func TestGiteaScopeRejects(t *testing.T) {
	for name, body := range map[string]string{
		"no type":     "scope:\n  gitea:\n    name: acme\n",
		"bad type":    "scope:\n  gitea:\n    type: group\n    name: acme\n",
		"no name":     "scope:\n  gitea:\n    type: org\n",
		"credentials": "scope:\n  gitea:\n    type: org\n    name: acme\n    base_url: https://u:pw@git.example.com\n",
		"unknown key": "scope:\n  gitea:\n    type: org\n    name: acme\n    group: x\n",
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
