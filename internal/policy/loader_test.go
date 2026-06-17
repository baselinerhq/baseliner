package policy

import (
	"os"
	"path/filepath"
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
	if len(p.Checks) != 10 {
		t.Errorf("got %d checks, want 10", len(p.Checks))
	}
	// Every default check is enabled and names a known severity.
	for _, c := range p.Checks {
		if !c.Enabled {
			t.Errorf("check %q unexpectedly disabled", c.ID)
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
