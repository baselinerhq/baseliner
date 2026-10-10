// Package policy loads baseliner policies. The default policy is embedded
// into the binary (no external data files needed at runtime).
package policy

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

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
	// Reject unknown keys rather than dropping them. Keys inside a check entry
	// are checked by CheckDefinition.UnmarshalYAML, which this decoder's
	// KnownFields setting does not reach.
	var p models.Policy
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parse policy %s: %w", source, err)
	}
	if p.ID == "" {
		return nil, fmt.Errorf("policy %s: missing id", source)
	}
	if len(p.Checks) == 0 {
		return nil, fmt.Errorf("policy %s: no checks defined", source)
	}
	for _, c := range p.Checks {
		if err := validateCheck(c); err != nil {
			return nil, fmt.Errorf("policy %s: check %q: %w", source, c.ID, err)
		}
	}
	return &p, nil
}

// FilePresent is the type of a check that passes when any of its paths
// exists.
const FilePresent = "file_present"

// maxPathDepth is how deep a file_present path may be: a local scan reads no
// deeper.
const maxPathDepth = 4

// validateCheck rejects a policy-defined check that could not run as
// written. A path is exact and repo-relative, so it names one file.
func validateCheck(c models.CheckDefinition) error {
	switch c.Type {
	case "":
		if len(c.AnyOf) > 0 {
			return fmt.Errorf("any_of needs type: %s", FilePresent)
		}
		return nil
	case FilePresent:
	default:
		return fmt.Errorf("unknown type %q (the one type is %s)", c.Type, FilePresent)
	}
	if c.ID == "" {
		return errors.New("missing id")
	}
	if len(c.AnyOf) == 0 {
		return errors.New("any_of must list at least one path")
	}
	for _, f := range c.AnyOf {
		switch {
		case f == "" || f == "." || strings.HasPrefix(f, "/") || strings.Contains(f, "\\") ||
			path.Clean(f) != f || f == ".." || strings.HasPrefix(f, "../"):
			return fmt.Errorf("any_of path %q: want a repo-relative file path such as .github/renovate.json", f)
		case strings.ContainsAny(f, "*?[{"):
			return fmt.Errorf("any_of path %q: globs are not supported; list each path", f)
		case strings.Count(f, "/")+1 > maxPathDepth:
			return fmt.Errorf("any_of path %q: deeper than %d path segments", f, maxPathDepth)
		}
	}
	return nil
}
