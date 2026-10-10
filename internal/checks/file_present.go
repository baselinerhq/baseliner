package checks

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/baselinerhq/baseliner/internal/models"
	"github.com/baselinerhq/baseliner/internal/policy"
)

// filePresent is a check a policy defines: it passes when any of its paths
// is a file in the repo.
type filePresent struct {
	base
	anyOf []string
}

func (c filePresent) Eval(repo *models.NormalizedRepository) models.CheckResult {
	for _, f := range c.anyOf {
		if slices.Contains(repo.FS.PolicyFiles, f) {
			return c.pass()
		}
	}
	// Absence is shown only where every directory a path sits in was read.
	for _, f := range c.anyOf {
		if d := parentDir(f); slices.Contains(repo.FS.PolicyUnreadDirs, d) {
			if d == "" {
				return unobservable(c.id, "repository root listing could not be read")
			}
			return unobservable(c.id, d+"/ listing could not be read")
		}
	}
	return c.fail("None of these files found: " + strings.Join(c.anyOf, ", "))
}

// parentDir returns the repo-relative directory a path sits in, "" for the
// root.
func parentDir(f string) string {
	if d := path.Dir(f); d != "." {
		return d
	}
	return ""
}

// ForPolicy returns the built-in checks plus each check the policy defines.
// A defined check may not take a built-in check's id.
func ForPolicy(pol *models.Policy) (*Registry, error) {
	r := BuildDefault()
	for _, def := range pol.Checks {
		if def.Type != policy.FilePresent {
			continue
		}
		if c, ok := r.Get(def.ID); ok {
			if _, defined := c.(filePresent); defined {
				return nil, fmt.Errorf("policy check %q is defined twice", def.ID)
			}
			return nil, fmt.Errorf("policy check %q: the id is a built-in check's", def.ID)
		}
		r.Register(filePresent{base{def.ID, LayerFS}, def.AnyOf})
	}
	return r, nil
}

// ExtraDirs returns the directories the policy's enabled file_present checks
// need listed, except checks in ignore, which run on no repo.
func ExtraDirs(pol *models.Policy, ignore []string) []string {
	var out []string
	for _, def := range pol.Checks {
		if def.Type != policy.FilePresent || !def.Enabled || slices.Contains(ignore, def.ID) {
			continue
		}
		for _, f := range def.AnyOf {
			if d := parentDir(f); !slices.Contains(out, d) {
				out = append(out, d)
			}
		}
	}
	slices.Sort(out)
	return out
}
