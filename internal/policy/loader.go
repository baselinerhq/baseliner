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
	"regexp"
	"slices"
	"strings"
	"unicode"

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
	dirs := map[string]bool{}
	for _, c := range p.Checks {
		if err := validateCheck(c); err != nil {
			return nil, fmt.Errorf("policy %s: check %q: %w", source, c.ID, err)
		}
		if !c.Enabled {
			continue
		}
		for _, f := range c.AnyOf {
			dirs[path.Dir(f)] = true
		}
	}
	// Each directory is one more request per repo on a forge.
	if len(dirs) > maxPolicyDirs {
		return nil, fmt.Errorf("policy %s: file_present paths sit in %d directories; at most %d", source, len(dirs), maxPolicyDirs)
	}
	return &p, nil
}

// FilePresent is the type of a check that passes when any of its paths
// exists.
const FilePresent = "file_present"

// maxPathDepth is how deep a file_present path may be: a local scan reads no
// deeper.
const maxPathDepth = 4

// maxPolicyDirs bounds the directories a policy's file_present paths may sit
// in, since each is listed on every repo.
const maxPolicyDirs = 20

// invisible reports a control or format character, such as a zero-width
// space or a right-to-left override, which would hide in a report.
func invisible(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }

// checkID is the form of a check id a policy defines: it reaches reports and
// issue titles, so it is a plain name.
var checkID = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]*$`)

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
	if !checkID.MatchString(c.ID) {
		return errors.New("id must be lowercase letters, digits, '_', '.' or '-'")
	}
	if len(c.AnyOf) == 0 {
		return errors.New("any_of must list at least one path")
	}
	for _, f := range c.AnyOf {
		switch {
		case f == "" || f == "." || strings.HasPrefix(f, "/") || strings.Contains(f, "\\") ||
			path.Clean(f) != f || f == ".." || strings.HasPrefix(f, "../"):
			return fmt.Errorf("any_of path %q: want a repo-relative file path such as .github/renovate.json", f)
		case strings.TrimSpace(f) != f || strings.ContainsFunc(f, invisible):
			return fmt.Errorf("any_of path %q: has surrounding spaces or an invisible character", f)
		case slices.Contains(strings.Split(f, "/"), ".git"):
			return fmt.Errorf("any_of path %q: a scan does not read .git", f)
		case strings.ContainsAny(f, "*?[{"):
			return fmt.Errorf("any_of path %q: globs are not supported; list each path", f)
		case strings.Count(f, "/")+1 > maxPathDepth:
			return fmt.Errorf("any_of path %q: deeper than %d path segments", f, maxPathDepth)
		}
	}
	return nil
}
