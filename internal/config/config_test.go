package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	if cfg.Privacy == nil || cfg.Privacy.PublicContext == nil || !*cfg.Privacy.PublicContext || cfg.Privacy.PrivateRepos != "exclude" {
		t.Errorf("privacy = %+v, want {public_context:true private_repos:exclude}", cfg.Privacy)
	}
}

// Unset and false are different: under GitHub Actions an unset
// public_context fails closed, and only an explicit false turns it off.
func TestPublicContextUnsetIsNil(t *testing.T) {
	cfg, err := Load(write(t, `
scope:
  local:
    paths: ["."]
privacy:
  private_repos: exclude
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Privacy == nil || cfg.Privacy.PublicContext != nil {
		t.Errorf("privacy = %+v, want public_context unset (nil)", cfg.Privacy)
	}
	cfg, err = Load(write(t, `
scope:
  local:
    paths: ["."]
privacy:
  public_context: false
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Privacy == nil || cfg.Privacy.PublicContext == nil || *cfg.Privacy.PublicContext {
		t.Errorf("privacy = %+v, want public_context explicitly false", cfg.Privacy)
	}
}

func TestIncludeArchivedParses(t *testing.T) {
	cfg, err := Load(write(t, `
scope:
  github:
    type: org
    name: acme
    include_archived: true
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Scope.GitHub.IncludeArchived {
		t.Error("include_archived = false, want true")
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

// An unknown key must be a config error, not silently dropped. A misspelled
// privacy key used to load as public_context=false — the guard off, with no
// error — and a misspelled repo_ignores used to drop every waiver.
func TestUnknownKeysRejected(t *testing.T) {
	cases := map[string]string{
		"top level": `
scope:
  local:
    paths: ["."]
polcy:
  base: default
`,
		"privacy typo": `
scope:
  local:
    paths: ["."]
privacy:
  public-context: true
`,
		"policy typo": `
scope:
  local:
    paths: ["."]
policy:
  repo_ignore:
    "/tmp/a": [ci_present]
`,
		"scope typo": `
scope:
  local:
    path: ["."]
`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load(write(t, body))
			var ce *ConfigError
			if !errors.As(err, &ce) {
				t.Fatalf("want ConfigError for unknown key, got %v", err)
			}
		})
	}
}

// An empty file is an empty config (then rejected for its missing scope), as it
// was before decoding became strict — not an EOF error.
func TestEmptyFileMissingScope(t *testing.T) {
	_, err := Load(write(t, ""))
	var ce *ConfigError
	if !errors.As(err, &ce) {
		t.Fatalf("want ConfigError, got %v", err)
	}
	if got := ce.Error(); got != "Config validation failed: scope is required" {
		t.Errorf("message = %q, want the missing-scope error", got)
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

func TestIgnoreWhenParses(t *testing.T) {
	cfg, err := Load(write(t, `
scope:
  local:
    paths: ["."]
policy:
  ignore_when:
    - visibility: [Private, internal]
      checks: [license_exists]
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	rules := cfg.Policy.IgnoreWhen
	if len(rules) != 1 || strings.Join(rules[0].Visibility, ",") != "private,internal" || strings.Join(rules[0].Checks, ",") != "license_exists" {
		t.Errorf("ignore_when = %+v, want one rule for private,internal on license_exists (visibility lowercased)", rules)
	}
}

// A rule that can match nothing is a mistake in the config, so it is an
// error rather than a waiver that silently never applies.
func TestIgnoreWhenRejectsBadRules(t *testing.T) {
	for name, rule := range map[string]string{
		"unknown visibility": "    - visibility: [secret]\n      checks: [license_exists]\n",
		"no visibility":      "    - checks: [license_exists]\n",
		"no checks":          "    - visibility: [private]\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(write(t, "scope:\n  local:\n    paths: [\".\"]\npolicy:\n  ignore_when:\n"+rule))
			var ce *ConfigError
			if !errors.As(err, &ce) {
				t.Errorf("want a ConfigError, got %v", err)
			}
		})
	}
}
