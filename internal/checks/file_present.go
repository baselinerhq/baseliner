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
		if slices.Contains(repo.FS.Files, f) {
			return c.pass()
		}
	}
	// Absence is shown only where every directory a path sits in was read.
	for _, f := range c.anyOf {
		if d := parentDir(f); slices.Contains(repo.FS.UnreadDirs, d) {
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
		if _, ok := r.Get(def.ID); ok {
			return nil, fmt.Errorf("policy check %q: the id is already a check", def.ID)
		}
		r.Register(filePresent{base{def.ID, LayerFS}, def.AnyOf})
	}
	return r, nil
}

// ExtraDirs returns the directories the policy's enabled file_present checks
// need listed, beyond those the collectors always list.
func ExtraDirs(pol *models.Policy) []string {
	var out []string
	for _, def := range pol.Checks {
		if def.Type != policy.FilePresent || !def.Enabled {
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
