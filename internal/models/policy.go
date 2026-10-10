package models

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// CheckDefinition is one entry in a policy: which check to run, at what
// severity, whether it is enabled, and optional context (why it matters / a link
// to the governing standard) surfaced on findings.
type CheckDefinition struct {
	ID         string   `yaml:"id" json:"id"`
	Severity   Severity `yaml:"severity" json:"severity"`
	Enabled    bool     `yaml:"enabled" json:"enabled"`
	PolicyInfo string   `yaml:"policy_info" json:"policy_info,omitempty"`
	PolicyURL  string   `yaml:"policy_url" json:"policy_url,omitempty"`
	// Type is empty for a built-in check, or "file_present" for a check the
	// policy defines, which passes when any path in AnyOf exists.
	Type  string   `yaml:"type" json:"type,omitempty"`
	AnyOf []string `yaml:"any_of" json:"any_of,omitempty"`
}

// UnmarshalYAML defaults Enabled to true when the key is absent, matching the
// Python pydantic field default (`enabled: bool = True`). Go's zero value for a
// bool is false, so without this a custom policy that omits `enabled:` would
// silently disable the check.
func (c *CheckDefinition) UnmarshalYAML(node *yaml.Node) error {
	type raw struct {
		ID         string   `yaml:"id"`
		Severity   Severity `yaml:"severity"`
		Enabled    *bool    `yaml:"enabled"`
		PolicyInfo string   `yaml:"policy_info"`
		PolicyURL  string   `yaml:"policy_url"`
		Type       string   `yaml:"type"`
		AnyOf      []string `yaml:"any_of"`
	}
	// node.Decode starts a fresh decoder, so the loader's KnownFields setting
	// does not reach here; reject unknown keys explicitly, or a misspelled
	// `severity` would be dropped without a word.
	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			switch k := node.Content[i]; k.Value {
			case "id", "severity", "enabled", "policy_info", "policy_url", "type", "any_of":
			default:
				return fmt.Errorf("line %d: unknown field %q in check definition", k.Line, k.Value)
			}
		}
	}
	r := raw{}
	if err := node.Decode(&r); err != nil {
		return err
	}
	c.ID = r.ID
	c.Severity = r.Severity
	c.Enabled = r.Enabled == nil || *r.Enabled
	c.PolicyInfo = r.PolicyInfo
	c.PolicyURL = r.PolicyURL
	c.Type = r.Type
	c.AnyOf = r.AnyOf
	return nil
}

// Policy is a named, versioned set of check definitions.
type Policy struct {
	ID     string            `yaml:"id" json:"id"`
	Checks []CheckDefinition `yaml:"checks" json:"checks"`
}
