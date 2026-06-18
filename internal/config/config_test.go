package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "baseliner.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(write(t, `
scope:
  github:
    type: org
    name: acme
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Policy.Base != "default" {
		t.Errorf("Base = %q, want default", cfg.Policy.Base)
	}
	if cfg.Scope.GitHub.TokenEnv != "GITHUB_TOKEN" {
		t.Errorf("TokenEnv = %q, want GITHUB_TOKEN", cfg.Scope.GitHub.TokenEnv)
	}
	if cfg.Scope.Local != nil {
		t.Errorf("Local should be nil when absent")
	}
}

func TestMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	var ce *ConfigError
	if !errors.As(err, &ce) {
		t.Fatalf("want ConfigError, got %v", err)
	}
	if got := ce.Error(); got[:len("Config file not found")] != "Config file not found" {
		t.Errorf("message = %q", got)
	}
}

func TestInvalidYAML(t *testing.T) {
	_, err := Load(write(t, "scope: [unclosed"))
	var ce *ConfigError
	if !errors.As(err, &ce) {
		t.Fatalf("want ConfigError, got %v", err)
	}
}

func TestMissingScope(t *testing.T) {
	_, err := Load(write(t, "policy:\n  base: default\n"))
	var ce *ConfigError
	if !errors.As(err, &ce) {
		t.Fatalf("want ConfigError for missing scope, got %v", err)
	}
}

func TestBadGitHubType(t *testing.T) {
	_, err := Load(write(t, "scope:\n  github:\n    type: team\n    name: acme\n"))
	var ce *ConfigError
	if !errors.As(err, &ce) {
		t.Fatalf("want ConfigError for bad type, got %v", err)
	}
}

func TestPrivacyConfigParses(t *testing.T) {
	cfg, err := Load(write(t, `
scope:
  github:
    type: org
    name: acme
privacy:
  public_context: true
  private_repos: exclude
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Privacy == nil || !cfg.Privacy.PublicContext || cfg.Privacy.PrivateRepos != "exclude" {
		t.Errorf("privacy = %+v, want {public_context:true private_repos:exclude}", cfg.Privacy)
	}
}

func TestPrivacyInvalidModeRejected(t *testing.T) {
	_, err := Load(write(t, `
scope:
  github:
    type: org
    name: acme
privacy:
  private_repos: bogus
`))
	var ce *ConfigError
	if !errors.As(err, &ce) {
		t.Fatalf("want ConfigError for bad privacy mode, got %v", err)
	}
}

func TestLocalScopeAndIgnores(t *testing.T) {
	cfg, err := Load(write(t, `
scope:
  local:
    paths: ["/tmp/a", "/tmp/b"]
policy:
  ignore: [stale_repo]
  repo_ignores:
    "/tmp/a": [ci_present]
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Scope.Local.Paths) != 2 {
		t.Errorf("paths = %v", cfg.Scope.Local.Paths)
	}
	if len(cfg.Policy.Ignore) != 1 || cfg.Policy.Ignore[0] != "stale_repo" {
		t.Errorf("ignore = %v", cfg.Policy.Ignore)
	}
	if got := cfg.Policy.RepoIgnores["/tmp/a"]; len(got) != 1 || got[0] != "ci_present" {
		t.Errorf("repo_ignores = %v", cfg.Policy.RepoIgnores)
	}
}
