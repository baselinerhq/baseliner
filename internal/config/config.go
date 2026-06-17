// Package config loads and validates baseliner.yaml.
package config

import (
	"errors"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"
)

// PolicyConfig mirrors the Python PolicyConfig.
type PolicyConfig struct {
	Base        string              `yaml:"base"`
	Ignore      []string            `yaml:"ignore"`
	RepoIgnores map[string][]string `yaml:"repo_ignores"`
}

// GitHubScope configures GitHub org/user discovery.
type GitHubScope struct {
	Type     string `yaml:"type"` // "org" or "user"
	Name     string `yaml:"name"`
	TokenEnv string `yaml:"token_env"`
}

// LocalScope configures local filesystem discovery.
type LocalScope struct {
	Paths []string `yaml:"paths"`
}

// Scope selects which repos to scan. github/local are nil when their key is absent.
type Scope struct {
	GitHub  *GitHubScope `yaml:"github"`
	Local   *LocalScope  `yaml:"local"`
	Include []string     `yaml:"include"`
	Exclude []string     `yaml:"exclude"`
}

// Config is the top-level baseliner.yaml model.
type Config struct {
	Scope  *Scope       `yaml:"scope"`
	Policy PolicyConfig `yaml:"policy"`
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

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
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
	return nil
}
