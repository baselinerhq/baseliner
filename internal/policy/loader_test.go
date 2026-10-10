package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefault(t *testing.T) {
	p, err := Load("default")
	if err != nil {
		t.Fatalf("Load(default): %v", err)
	}
	if p.ID != "default-v1" {
		t.Errorf("id = %q, want default-v1", p.ID)
	}
	if len(p.Checks) != 12 {
		t.Errorf("got %d checks, want 12", len(p.Checks))
	}
	// The platform checks ship disabled (they cost extra API calls and want an
	// admin-capable token); every other default check is enabled. All name a
	// known severity.
	optIn := map[string]bool{"default_branch_requires_review": true, "no_exempt_bypass": true}
	for _, c := range p.Checks {
		if c.Enabled == optIn[c.ID] {
			t.Errorf("check %q: enabled = %v, want %v", c.ID, c.Enabled, !optIn[c.ID])
		}
		if c.Severity.Weight() < 1 {
			t.Errorf("check %q has invalid severity %q", c.ID, c.Severity)
		}
	}
}

func TestLoadMissingPath(t *testing.T) {
	if _, err := Load("/no/such/policy.yaml"); err == nil {
		t.Fatal("expected error loading missing policy path")
	}
}

// Unknown keys in a custom policy — at the top level or inside a check entry —
// must be rejected rather than silently dropped. A misspelled `severity` inside a
// check is decoded by CheckDefinition.UnmarshalYAML, which a strict outer decoder
// does not reach, so it is covered separately.
func TestUnknownKeysRejected(t *testing.T) {
	cases := map[string]string{
		"top level": `id: custom-v1
descripton: misspelled, otherwise valid
checks:
  - id: readme_exists
    severity: critical
`,
		"in check": `id: custom-v1
checks:
  - id: readme_exists
    serverity: critical
`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "custom.yaml")
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("expected error for unknown key, got nil")
			}
		})
	}
}

// A custom policy that omits `enabled:` must default to enabled (parity with
// pydantic's `enabled: bool = True`), not Go's zero value false.
func TestOmittedEnabledDefaultsTrue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom.yaml")
	const body = `id: custom-v1
checks:
  - id: readme_exists
    severity: critical
  - id: license_exists
    severity: high
    enabled: false
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Load(path)
	if err != nil {
		t.Fatalf("Load(custom): %v", err)
	}
	if len(p.Checks) != 2 {
		t.Fatalf("got %d checks, want 2", len(p.Checks))
	}
	if !p.Checks[0].Enabled {
		t.Errorf("readme_exists: enabled = false, want true (omitted key defaults true)")
	}
	if p.Checks[1].Enabled {
		t.Errorf("license_exists: enabled = true, want false (explicitly disabled)")
	}
}

// A file_present check needs exact repo-relative paths a scan can read; any
// other shape is refused at load rather than failing or passing every repo.
func TestFilePresentValidation(t *testing.T) {
	load := func(check string) error {
		path := filepath.Join(t.TempDir(), "custom.yaml")
		if err := os.WriteFile(path, []byte("id: custom-v1\nchecks:\n  - "+check+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := Load(path)
		return err
	}
	if err := load(`{id: renovate, type: file_present, severity: medium, any_of: [renovate.json, .github/renovate.json, a/b/c/d.json]}`); err != nil {
		t.Errorf("valid check refused: %v", err)
	}
	for _, bad := range []string{
		`{id: r, type: file_present, severity: medium}`,
		`{id: r, type: file_present, severity: medium, any_of: []}`,
		`{id: r, type: file_glob, severity: medium, any_of: [x]}`,
		`{id: readme_exists, severity: medium, any_of: [x]}`,
		`{type: file_present, severity: medium, any_of: [x]}`,
		`{id: r, type: file_present, any_of: [""]}`,
		`{id: r, type: file_present, any_of: [/etc/passwd]}`,
		`{id: r, type: file_present, any_of: [../x]}`,
		`{id: r, type: file_present, any_of: [..]}`,
		`{id: r, type: file_present, any_of: [.]}`,
		`{id: r, type: file_present, any_of: [./x]}`,
		`{id: r, type: file_present, any_of: [a/../x]}`,
		`{id: r, type: file_present, any_of: [config/]}`,
		`{id: r, type: file_present, any_of: [a//b]}`,
		`{id: r, type: file_present, any_of: ['a\b']}`,
		`{id: r, type: file_present, any_of: ["*.json"]}`,
		`{id: r, type: file_present, any_of: ["a/b/c/d/e.json"]}`,
		`{id: r, type: file_present, any_of: [".git/config"]}`,
		`{id: r, type: file_present, any_of: ["sub/.git/HEAD"]}`,
		`{id: r, type: file_present, any_of: ["vendor/lib/.git"]}`,
		`{id: r, type: file_present, any_of: ["a\u200bb"]}`,
		`{id: r, type: file_present, any_of: ["a\u202eb"]}`,
		`{id: r, type: file_present, any_of: ["trail "]}`,
		`{id: r, type: file_present, any_of: ["a\nb"]}`,
		`{id: "my check", type: file_present, any_of: [x]}`,
		`{id: "Renovate", type: file_present, any_of: [x]}`,
	} {
		if err := load(bad); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

// Each directory an enabled check's paths sit in is listed on every repo,
// so their number is bounded: 20 are accepted, 21 refused, and a disabled
// check's do not count.
func TestFilePresentDirectoryCap(t *testing.T) {
	load := func(n int, enabled bool) error {
		var b strings.Builder
		fmt.Fprintf(&b, "id: custom-v1\nchecks:\n  - {id: many, type: file_present, enabled: %v, any_of: [", enabled)
		for i := range n {
			fmt.Fprintf(&b, "d%d/x, ", i)
		}
		b.WriteString("]}\n")
		path := filepath.Join(t.TempDir(), "custom.yaml")
		if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := Load(path)
		return err
	}
	if err := load(20, true); err != nil {
		t.Errorf("20 directories refused: %v", err)
	}
	if err := load(21, true); err == nil || !strings.Contains(err.Error(), "at most 20") {
		t.Errorf("21 directories: err = %v", err)
	}
	if err := load(21, false); err != nil {
		t.Errorf("a disabled check's directories counted: %v", err)
	}
}
