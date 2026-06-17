// Package introspect renders the check catalog and the effective policy for the
// `baseliner checks` and `baseliner policy` commands.
package introspect

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	"github.com/baselinerhq/baseliner/internal/checks"
	"github.com/baselinerhq/baseliner/internal/config"
	"github.com/baselinerhq/baseliner/internal/models"
	"github.com/baselinerhq/baseliner/internal/policy"
)

// CheckRow is one check's catalog/effective entry.
type CheckRow struct {
	ID       string `json:"id"`
	Layer    string `json:"layer"`
	Severity string `json:"severity"`
	Enabled  bool   `json:"enabled"`
}

// Catalog lists every built-in check with the severity and enabled state it has
// in the default policy.
func Catalog() ([]CheckRow, error) {
	def, err := policy.Load("default")
	if err != nil {
		return nil, err
	}
	return rows(checks.BuildDefault(), def), nil
}

// EffectivePolicy is the resolved policy for a config: which checks run, at what
// severity, plus the ignore rules that suppress them.
type EffectivePolicy struct {
	PolicyID      string              `json:"policy_id"`
	Checks        []CheckRow          `json:"checks"`
	GlobalIgnores []string            `json:"global_ignores"`
	RepoIgnores   map[string][]string `json:"repo_ignores"`
}

// Effective resolves the policy and ignore rules for the given config path.
func Effective(configPath string) (*EffectivePolicy, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	pol, err := policy.Load(cfg.Policy.Base)
	if err != nil {
		return nil, err
	}
	gi := cfg.Policy.Ignore
	if gi == nil {
		gi = []string{}
	}
	ri := cfg.Policy.RepoIgnores
	if ri == nil {
		ri = map[string][]string{}
	}
	return &EffectivePolicy{
		PolicyID:      pol.ID,
		Checks:        rows(checks.BuildDefault(), pol),
		GlobalIgnores: gi,
		RepoIgnores:   ri,
	}, nil
}

// rows builds CheckRows for a policy, pulling each check's layer from the registry.
func rows(reg *checks.Registry, pol *models.Policy) []CheckRow {
	out := make([]CheckRow, 0, len(pol.Checks))
	for _, def := range pol.Checks {
		layer := ""
		if c, ok := reg.Get(def.ID); ok {
			layer = string(c.Layer())
		}
		out = append(out, CheckRow{
			ID:       def.ID,
			Layer:    layer,
			Severity: string(def.Severity),
			Enabled:  def.Enabled,
		})
	}
	return out
}

// WriteJSON writes v as indented JSON (no HTML escaping), with a trailing newline.
func WriteJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// WriteChecksTable writes a human-readable table of check rows.
func WriteChecksTable(w io.Writer, checkRows []CheckRow) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "CHECK\tLAYER\tSEVERITY\tENABLED")
	for _, r := range checkRows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%t\n", r.ID, r.Layer, r.Severity, r.Enabled)
	}
	_ = tw.Flush()
}

// WritePolicyTable writes the effective policy: its id, the check table, and any
// ignore rules.
func WritePolicyTable(w io.Writer, p *EffectivePolicy) {
	fmt.Fprintf(w, "policy: %s\n\n", p.PolicyID)
	WriteChecksTable(w, p.Checks)
	if len(p.GlobalIgnores) > 0 {
		fmt.Fprintf(w, "\nglobal ignores: %v\n", p.GlobalIgnores)
	}
	if len(p.RepoIgnores) > 0 {
		fmt.Fprintln(w, "\nrepo ignores:")
		keys := make([]string, 0, len(p.RepoIgnores))
		for k := range p.RepoIgnores {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(w, "  %s: %v\n", k, p.RepoIgnores[k])
		}
	}
}
