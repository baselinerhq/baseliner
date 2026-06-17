package output

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/baselinerhq/baseliner/internal/models"
)

func TestBuildSARIFStructure(t *testing.T) {
	log := buildSARIF(sampleRun())

	if log.Version != "2.1.0" {
		t.Errorf("version = %q, want 2.1.0", log.Version)
	}
	if log.Schema == "" {
		t.Error("$schema is empty")
	}
	if len(log.Runs) != 1 {
		t.Fatalf("got %d runs, want 1", len(log.Runs))
	}
	run := log.Runs[0]
	if run.Tool.Driver.Name != "baseliner" {
		t.Errorf("driver name = %q", run.Tool.Driver.Name)
	}
	if len(run.Tool.Driver.Rules) != 10 {
		t.Errorf("got %d rules, want 10", len(run.Tool.Driver.Rules))
	}

	// sampleRun has exactly one failing check (acme/bad readme_exists); passes and
	// skips must not produce results.
	if len(run.Results) != 1 {
		t.Fatalf("got %d results, want 1 (only the failing check)", len(run.Results))
	}
	r := run.Results[0]
	if r.RuleID != "readme_exists" || r.Level != "error" {
		t.Errorf("result = {ruleId:%q level:%q}, want readme_exists/error", r.RuleID, r.Level)
	}
	if r.Message.Text != "No README file found" {
		t.Errorf("message = %q", r.Message.Text)
	}
	if len(r.Locations) == 0 || r.Locations[0].PhysicalLocation.ArtifactLocation.URI != "acme/bad" {
		t.Errorf("location = %+v, want uri acme/bad", r.Locations)
	}
	if r.Properties["repo"] != "acme/bad" || r.Properties["severity"] != "critical" {
		t.Errorf("properties = %v", r.Properties)
	}

	// every rule has the required metadata
	for _, rule := range run.Tool.Driver.Rules {
		if rule.ID == "" || rule.Name == "" || rule.ShortDescription.Text == "" || rule.DefaultConfiguration.Level == "" {
			t.Errorf("incomplete rule: %+v", rule)
		}
	}
}

func TestSeverityToLevel(t *testing.T) {
	cases := map[models.Severity]string{
		models.SeverityCritical: "error",
		models.SeverityHigh:     "error",
		models.SeverityMedium:   "warning",
		models.SeverityLow:      "note",
		models.Severity("???"):  "warning",
	}
	for sev, want := range cases {
		if got := severityToLevel(sev); got != want {
			t.Errorf("severityToLevel(%q) = %q, want %q", sev, got, want)
		}
	}
}

func TestWriteSARIFFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.sarif")
	if err := WriteSARIF(sampleRun(), path); err != nil {
		t.Fatalf("WriteSARIF: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var log sarifLog
	if err := json.Unmarshal(data, &log); err != nil {
		t.Fatalf("written SARIF is not valid JSON: %v", err)
	}
	if log.Version != "2.1.0" || len(log.Runs) != 1 {
		t.Errorf("round-trip mismatch: version=%q runs=%d", log.Version, len(log.Runs))
	}
}
