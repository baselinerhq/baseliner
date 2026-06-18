package models

import "gopkg.in/yaml.v3"

// CheckDefinition is one entry in a policy: which check to run, at what
// severity, whether it is enabled, and optional context (why it matters / a link
// to the governing standard) surfaced on findings.
type CheckDefinition struct {
	ID         string   `yaml:"id" json:"id"`
	Severity   Severity `yaml:"severity" json:"severity"`
	Enabled    bool     `yaml:"enabled" json:"enabled"`
	PolicyInfo string   `yaml:"policy_info" json:"policy_info,omitempty"`
	PolicyURL  string   `yaml:"policy_url" json:"policy_url,omitempty"`
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
	return nil
}

// Policy is a named, versioned set of check definitions.
type Policy struct {
	ID     string            `yaml:"id" json:"id"`
	Checks []CheckDefinition `yaml:"checks" json:"checks"`
}
