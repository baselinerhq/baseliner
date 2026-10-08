package introspect

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatalog(t *testing.T) {
	rows, err := Catalog()
	if err != nil {
		t.Fatalf("Catalog: %v", err)
	}
	if len(rows) != 12 {
		t.Fatalf("got %d checks, want 12", len(rows))
	}
	want := map[string]CheckRow{
		"readme_exists":    {ID: "readme_exists", Layer: "fs", Severity: "critical", Enabled: true},
		"stale_repo":       {ID: "stale_repo", Layer: "git", Severity: "low", Enabled: true},
		"no_exempt_bypass": {ID: "no_exempt_bypass", Layer: "platform", Severity: "high", Enabled: false},
	}
	got := map[string]CheckRow{}
	for _, r := range rows {
		got[r.ID] = r
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s = %+v, want %+v", id, got[id], w)
		}
	}
}

func TestEffective(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "baseliner.yaml")
	body := `scope:
  github:
    type: org
    name: acme
policy:
  base: default
  ignore: [stale_repo]
  repo_ignores:
    "acme/infra": [ci_present, gitignore_exists]
  ignore_when:
    - visibility: [private, internal]
      checks: [license_exists]
`
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	eff, err := Effective(cfg)
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}
	if eff.PolicyID != "default-v1" {
		t.Errorf("policy id = %q, want default-v1", eff.PolicyID)
	}
	if len(eff.Checks) != 12 {
		t.Errorf("got %d checks, want 12", len(eff.Checks))
	}
	if len(eff.GlobalIgnores) != 1 || eff.GlobalIgnores[0] != "stale_repo" {
		t.Errorf("global ignores = %v", eff.GlobalIgnores)
	}
	if ri := eff.RepoIgnores["acme/infra"]; len(ri) != 2 {
		t.Errorf("repo ignores for acme/infra = %v", ri)
	}
	if len(eff.IgnoreWhen) != 1 || len(eff.IgnoreWhen[0].Visibility) != 2 || eff.IgnoreWhen[0].Checks[0] != "license_exists" {
		t.Errorf("ignore_when = %+v", eff.IgnoreWhen)
	}
	var buf bytes.Buffer
	WritePolicyTable(&buf, eff)
	if !strings.Contains(buf.String(), "ignored when visibility is private, internal: [license_exists]") {
		t.Errorf("policy table does not show the visibility rule:\n%s", buf.String())
	}
}

func TestEffectiveMissingConfig(t *testing.T) {
	if _, err := Effective("/no/such/baseliner.yaml"); err == nil {
		t.Fatal("expected an error for a missing config")
	}
}

func TestRenderTableAndJSON(t *testing.T) {
	rows, _ := Catalog()
	var tbl bytes.Buffer
	WriteChecksTable(&tbl, rows)
	out := tbl.String()
	for _, want := range []string{"CHECK", "LAYER", "SEVERITY", "ENABLED", "readme_exists", "critical"} {
		if !bytes.Contains(tbl.Bytes(), []byte(want)) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}

	var js bytes.Buffer
	if err := WriteJSON(&js, rows); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(js.Bytes(), []byte(`"id": "readme_exists"`)) {
		t.Errorf("json missing readme_exists:\n%s", js.String())
	}
}
