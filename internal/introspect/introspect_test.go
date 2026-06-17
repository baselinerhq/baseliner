package introspect

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCatalog(t *testing.T) {
	rows, err := Catalog()
	if err != nil {
		t.Fatalf("Catalog: %v", err)
	}
	if len(rows) != 10 {
		t.Fatalf("got %d checks, want 10", len(rows))
	}
	want := map[string]CheckRow{
		"readme_exists": {ID: "readme_exists", Layer: "fs", Severity: "critical", Enabled: true},
		"stale_repo":    {ID: "stale_repo", Layer: "git", Severity: "low", Enabled: true},
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
	if len(eff.Checks) != 10 {
		t.Errorf("got %d checks, want 10", len(eff.Checks))
	}
	if len(eff.GlobalIgnores) != 1 || eff.GlobalIgnores[0] != "stale_repo" {
		t.Errorf("global ignores = %v", eff.GlobalIgnores)
	}
	if ri := eff.RepoIgnores["acme/infra"]; len(ri) != 2 {
		t.Errorf("repo ignores for acme/infra = %v", ri)
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
