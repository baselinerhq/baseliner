// Package config loads and validates baseliner.yaml.
package config

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/baselinerhq/baseliner/internal/privacy"
)

// PolicyConfig mirrors the Python PolicyConfig.
type PolicyConfig struct {
	Base        string              `yaml:"base"`
	Ignore      []string            `yaml:"ignore"`
	RepoIgnores map[string][]string `yaml:"repo_ignores"`
	// IgnoreWhen waives checks on every repo whose visibility a rule lists,
	// without naming any repo.
	IgnoreWhen []VisibilityRule `yaml:"ignore_when"`
	// RepoWaivers decides whether a repo's own .baseliner.yml waivers apply.
	// Off unless set.
	RepoWaivers *RepoWaiversConfig `yaml:"repo_waivers"`
}

// RepoWaiversConfig lists the checks a repo may waive for itself. A waiver of
// any other check is ignored.
type RepoWaiversConfig struct {
	Allow []string `yaml:"allow" json:"allow"`
}

// VisibilityRule waives Checks on repos whose GitHub visibility is one of
// Visibility (public, private or internal). A repo with no visibility, such
// as a local checkout, matches no rule.
type VisibilityRule struct {
	Visibility []string `yaml:"visibility" json:"visibility"`
	Checks     []string `yaml:"checks" json:"checks"`
}

// GitHubScope configures GitHub org/user discovery.
type GitHubScope struct {
	Type     string `yaml:"type"` // "org" or "user"
	Name     string `yaml:"name"`
	TokenEnv string `yaml:"token_env"`
	// IncludeArchived scans archived repos too. Off by default: an archived
	// repo is read-only, so once it goes stale it fails permanently.
	IncludeArchived bool `yaml:"include_archived"`
}

// GitLabScope configures GitLab group discovery, on gitlab.com or a
// self-managed instance.
type GitLabScope struct {
	// Group is the group's full path, such as "acme" or "acme/platform"; its
	// subgroups' projects are included.
	Group string `yaml:"group"`
	// BaseURL is the instance's root URL (default https://gitlab.com). The
	// token is sent only there.
	BaseURL  string `yaml:"base_url"`
	TokenEnv string `yaml:"token_env"`
	// IncludeArchived scans archived projects too, as for GitHub.
	IncludeArchived bool `yaml:"include_archived"`
}

// LocalScope configures local filesystem discovery.
type LocalScope struct {
	Paths []string `yaml:"paths"`
}

// Scope selects which repos to scan. github/gitlab/local are nil when their
// key is absent.
type Scope struct {
	GitHub  *GitHubScope `yaml:"github"`
	GitLab  *GitLabScope `yaml:"gitlab"`
	Local   *LocalScope  `yaml:"local"`
	Include []string     `yaml:"include"`
	Exclude []string     `yaml:"exclude"`
}

// PrivacyConfig controls how private/internal repos are disclosed when a scan's
// output goes to a public place (e.g. a public control repo's Actions logs and
// artifacts). When PublicContext is true, PrivateRepos selects the treatment.
type PrivacyConfig struct {
	// PublicContext signals that the output is public. The GitHub Action sets
	// it from the control repo's visibility; raw CLI users set it here or via
	// --public-context. Unset (nil) is false, except under GitHub Actions,
	// where it is true: only an explicit false turns the guard off there.
	PublicContext *bool `yaml:"public_context"`
	// PrivateRepos is the treatment for private/internal repos in a public
	// context: allow | redact | exclude | fail. Empty defaults to redact.
	PrivateRepos string `yaml:"private_repos"`
}

// Config is the top-level baseliner.yaml model.
type Config struct {
	Scope   *Scope         `yaml:"scope"`
	Policy  PolicyConfig   `yaml:"policy"`
	Privacy *PrivacyConfig `yaml:"privacy"`
}

// Load reads, parses, validates, and default-fills a config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, NewConfigError("Config file not found: %s", path)
		}
		return nil, NewConfigError("Could not read config file: %v", err)
	}

	// Reject unknown keys: a misspelled key would otherwise be dropped and its
	// setting fall back to the default — for privacy.public_context, silently
	// turning the privacy guard off. An empty file (io.EOF) stays an empty
	// config, as it was with yaml.Unmarshal.
	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, NewConfigError("Invalid YAML in config file: %v", err)
	}

	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Policy.Base == "" {
		c.Policy.Base = "default"
	}
	if c.Scope != nil && c.Scope.GitHub != nil && c.Scope.GitHub.TokenEnv == "" {
		c.Scope.GitHub.TokenEnv = "GITHUB_TOKEN"
	}
	if c.Scope != nil && c.Scope.GitLab != nil {
		if c.Scope.GitLab.TokenEnv == "" {
			c.Scope.GitLab.TokenEnv = "GITLAB_TOKEN"
		}
		if c.Scope.GitLab.BaseURL == "" {
			c.Scope.GitLab.BaseURL = "https://gitlab.com"
		}
	}
}

// ValidateCheckIDs reports a policy.ignore_when rule naming a check that known
// does not recognise, an empty ID, or one repeated within a rule: such a rule
// cannot waive what it says, so it is a config error. It is separate from Load
// because the config package does not know the check registry.
func (c *Config) ValidateCheckIDs(known func(id string) bool) error {
	if rw := c.Policy.RepoWaivers; rw != nil {
		if len(rw.Allow) == 0 {
			return NewConfigError("Config validation failed: policy.repo_waivers.allow must list the checks repos may waive")
		}
		seen := map[string]bool{}
		for _, id := range rw.Allow {
			switch {
			case id == "":
				return NewConfigError("Config validation failed: policy.repo_waivers.allow has an empty check id")
			case seen[id]:
				return NewConfigError("Config validation failed: policy.repo_waivers.allow lists %q twice", id)
			case !known(id):
				return NewConfigError("Config validation failed: policy.repo_waivers.allow: unknown check %q (see `baseliner checks`)", id)
			}
			seen[id] = true
		}
	}
	for i, rule := range c.Policy.IgnoreWhen {
		seen := map[string]bool{}
		for _, id := range rule.Checks {
			switch {
			case id == "":
				return NewConfigError("Config validation failed: policy.ignore_when[%d].checks has an empty check id", i)
			case seen[id]:
				return NewConfigError("Config validation failed: policy.ignore_when[%d].checks lists %q twice", i, id)
			case !known(id):
				return NewConfigError("Config validation failed: policy.ignore_when[%d].checks: unknown check %q (see `baseliner checks`)", i, id)
			}
			seen[id] = true
		}
	}
	return nil
}

func (c *Config) validate() error {
	if c.Scope == nil {
		return NewConfigError("Config validation failed: scope is required")
	}
	if gh := c.Scope.GitHub; gh != nil {
		if gh.Type != "org" && gh.Type != "user" {
			return NewConfigError("Config validation failed: scope.github.type must be 'org' or 'user'")
		}
		if gh.Name == "" {
			return NewConfigError("Config validation failed: scope.github.name is required")
		}
	}
	if gl := c.Scope.GitLab; gl != nil {
		gl.Group = strings.Trim(strings.TrimSpace(gl.Group), "/")
		if gl.Group == "" {
			return NewConfigError("Config validation failed: scope.gitlab.group is required")
		}
		u, err := url.Parse(strings.TrimSpace(gl.BaseURL))
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			// The value is not echoed: it may carry credentials.
			return NewConfigError("Config validation failed: scope.gitlab.base_url must be an http(s) URL without credentials, query or fragment, such as https://gitlab.example.com")
		}
		u.Path = strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), "/api/v4")
		u.RawPath = ""
		gl.BaseURL = strings.TrimSuffix(u.String(), "/")
	}
	if c.Privacy != nil {
		if _, err := privacy.ParseMode(c.Privacy.PrivateRepos); err != nil {
			return NewConfigError("Config validation failed: %v", err)
		}
	}
	for i := range c.Policy.IgnoreWhen {
		rule := &c.Policy.IgnoreWhen[i]
		if len(rule.Visibility) == 0 || len(rule.Checks) == 0 {
			return NewConfigError("Config validation failed: policy.ignore_when[%d] needs both visibility and checks", i)
		}
		for j, v := range rule.Visibility {
			v = strings.ToLower(v)
			if v != "public" && v != "private" && v != "internal" {
				return NewConfigError("Config validation failed: policy.ignore_when[%d].visibility %q must be public, private or internal", i, rule.Visibility[j])
			}
			rule.Visibility[j] = v
		}
	}
	return nil
}
