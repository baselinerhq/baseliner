// Package waivers reads the waivers a repo declares about itself, in a
// .baseliner.yml (or .baseliner.yaml) file at its root. Whether they are
// honoured, and for which checks, is decided by the central policy
// (policy.repo_waivers).
package waivers

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/baselinerhq/baseliner/internal/models"
)

// Path is the documented name of the waiver file, relative to the repo root.
// A root file works on every forge.
const Path = ".baseliner.yml"

// Names are the file names read, in order; a repo with more than one is
// ambiguous and declares no waivers.
var Names = []string{".baseliner.yml", ".baseliner.yaml"}

// MaxBytes bounds the file; a larger one is rejected rather than truncated.
const MaxBytes = 64 << 10

// MaxReasonLen bounds a reason, which is shown in reports and issues.
const MaxReasonLen = 300

// Version is the newest file format this baseliner reads.
const Version = 1

type file struct {
	Version int     `yaml:"version"`
	Waivers []entry `yaml:"waivers"`
}

type entry struct {
	Check  string `yaml:"check"`
	Reason string `yaml:"reason"`
	Until  string `yaml:"until"`
}

var lineRe = regexp.MustCompile(`line (\d+)`)

// Parse reads a waiver file. An empty file declares no waivers. It is strict:
// an unknown key, a second YAML document, a newer format version, a waiver
// without a check or a reason, a reason over MaxReasonLen, an until that is
// not a YYYY-MM-DD date, a check waived twice, or a file over MaxBytes is an
// error. Errors give positions, never the file's values, since the file may
// belong to a private repo and the error may reach a public log.
func Parse(data []byte) ([]models.Waiver, error) {
	if len(data) > MaxBytes {
		return nil, fmt.Errorf("waiver file is larger than %d KiB", MaxBytes>>10)
	}
	var f file
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil // an empty file
		}
		where := ""
		if m := lineRe.FindStringSubmatch(err.Error()); m != nil {
			where = " (line " + m[1] + ")"
		}
		return nil, fmt.Errorf("waiver file is not valid YAML, or has an unknown key or a value of the wrong type%s", where)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("waiver file has more than one YAML document")
	}
	if f.Version > Version {
		return nil, fmt.Errorf("waiver file is version %d; this baseliner reads up to version %d", f.Version, Version)
	}
	if f.Version < 0 {
		return nil, errors.New("waiver file version must be positive")
	}
	seen := map[string]bool{}
	out := make([]models.Waiver, 0, len(f.Waivers))
	for i, e := range f.Waivers {
		check, reason := strings.TrimSpace(e.Check), strings.TrimSpace(e.Reason)
		switch {
		case check == "":
			return nil, fmt.Errorf("waivers[%d] has no check", i)
		case reason == "":
			return nil, fmt.Errorf("waivers[%d] has no reason", i)
		case len([]rune(reason)) > MaxReasonLen:
			return nil, fmt.Errorf("waivers[%d] has a reason over %d characters", i, MaxReasonLen)
		case seen[check]:
			return nil, fmt.Errorf("waivers[%d] waives a check an earlier waiver already waives", i)
		}
		seen[check] = true
		w := models.Waiver{Check: check, Reason: reason}
		if e.Until != "" {
			t, err := time.Parse("2006-01-02", strings.TrimSpace(e.Until))
			if err != nil {
				return nil, fmt.Errorf("waivers[%d] has an until that is not a YYYY-MM-DD date", i)
			}
			w.Until = &t
		}
		out = append(out, w)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}
