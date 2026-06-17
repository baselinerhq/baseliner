// Package policy loads baseliner policies. The default policy is embedded
// into the binary (no external data files needed at runtime).
package policy

import (
	_ "embed"
	"fmt"
	"os"

	"github.com/baselinerhq/baseliner/internal/models"
	"gopkg.in/yaml.v3"
)

//go:embed default.yaml
var defaultPolicyYAML []byte

// Load resolves a policy by base: "default" loads the embedded policy,
// anything else is treated as a path to a custom policy YAML file.
func Load(base string) (*models.Policy, error) {
	if base == "" || base == "default" {
		return parse(defaultPolicyYAML, "<embedded default>")
	}
	data, err := os.ReadFile(base)
	if err != nil {
		return nil, fmt.Errorf("read policy %q: %w", base, err)
	}
	return parse(data, base)
}

func parse(data []byte, source string) (*models.Policy, error) {
	var p models.Policy
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse policy %s: %w", source, err)
	}
	if p.ID == "" {
		return nil, fmt.Errorf("policy %s: missing id", source)
	}
	if len(p.Checks) == 0 {
		return nil, fmt.Errorf("policy %s: no checks defined", source)
	}
	return &p, nil
}
