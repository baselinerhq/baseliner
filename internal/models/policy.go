package models

// CheckDefinition is one entry in a policy: which check to run, at what
// severity, and whether it is enabled.
type CheckDefinition struct {
	ID       string   `yaml:"id" json:"id"`
	Severity Severity `yaml:"severity" json:"severity"`
	Enabled  bool     `yaml:"enabled" json:"enabled"`
}

// Policy is a named, versioned set of check definitions.
type Policy struct {
	ID     string            `yaml:"id" json:"id"`
	Checks []CheckDefinition `yaml:"checks" json:"checks"`
}
