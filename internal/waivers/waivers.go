// Package waivers reads the waivers a repo declares about itself, in a
// .baseliner.yml file at its root. Whether they are honoured, and for which
// checks, is decided by the central policy (policy.repo_waivers).
package waivers

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/baselinerhq/baseliner/internal/models"
)

// Path is where a repo declares its waivers, relative to its root. A root
// file works on every forge.
const Path = ".baseliner.yml"

// MaxBytes bounds how much of the file is read.
const MaxBytes = 64 << 10

type file struct {
	Waivers []entry `yaml:"waivers"`
}

type entry struct {
	Check  string `yaml:"check"`
	Reason string `yaml:"reason"`
	Until  string `yaml:"until"`
}

// Parse reads a waiver file. It is strict: an unknown key, a waiver without a
// check or a reason, an until that is not a YYYY-MM-DD date, or a check
// waived twice is an error.
func Parse(data []byte) ([]models.Waiver, error) {
	var f file
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", Path, err)
	}
	if len(f.Waivers) == 0 {
		return nil, errors.New(Path + " has no waivers")
	}
	seen := map[string]bool{}
	out := make([]models.Waiver, 0, len(f.Waivers))
	for i, e := range f.Waivers {
		switch {
		case e.Check == "":
			return nil, fmt.Errorf("%s: waivers[%d] has no check", Path, i)
		case e.Reason == "":
			return nil, fmt.Errorf("%s: waivers[%d] (%s) has no reason", Path, i, e.Check)
		case seen[e.Check]:
			return nil, fmt.Errorf("%s: %s is waived twice", Path, e.Check)
		}
		seen[e.Check] = true
		w := models.Waiver{Check: e.Check, Reason: e.Reason}
		if e.Until != "" {
			t, err := time.Parse("2006-01-02", e.Until)
			if err != nil {
				return nil, fmt.Errorf("%s: waivers[%d] (%s): until %q is not a YYYY-MM-DD date", Path, i, e.Check, e.Until)
			}
			w.Until = &t
		}
		out = append(out, w)
	}
	return out, nil
}
