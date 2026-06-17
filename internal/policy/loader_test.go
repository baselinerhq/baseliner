package policy

import "testing"

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
