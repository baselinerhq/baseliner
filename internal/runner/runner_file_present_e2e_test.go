package runner

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/baselinerhq/baseliner/internal/models"
)

// A policy's file_present check runs end to end: the collector lists the
// directory its path sits in, so a file there passes and a missing one fails.
func TestScanFilePresentCheck(t *testing.T) {
	inner := fakeGitHub(t)
	var opsListed atomic.Bool
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/contents/ops") {
			opsListed.Store(true)
		}
		if r.URL.Path == "/repos/acme/open-kit/contents/config" {
			_, _ = w.Write([]byte(`[{"type":"file","path":"config/renovate.json"}]`))
			return
		}
		inner.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(gh.Close)
	t.Setenv("GITHUB_API_URL", gh.URL)
	t.Setenv("GITHUB_TOKEN", "test-token")
	dir := t.TempDir()
	pol := filepath.Join(dir, "policy.yaml")
	body := "id: custom-v1\nchecks:\n" +
		"  - {id: readme_exists, severity: critical}\n" +
		"  - {id: renovate_config, type: file_present, severity: medium, any_of: [renovate.json, config/renovate.json]}\n" +
		"  - {id: ops_manifest, type: file_present, severity: low, any_of: [ops/manifest.yaml]}\n"
	if err := os.WriteFile(pol, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "baseliner.yaml")
	if err := os.WriteFile(cfg, []byte("scope:\n  github:\n    type: org\n    name: acme\npolicy:\n  base: "+pol+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := run(Options{ConfigPath: cfg, Format: "json"})
	if code != 1 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	var res models.RunResult
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatal(err)
	}
	got := map[string]models.CheckStatus{}
	for _, r := range res.Repos {
		for _, c := range r.Results {
			got[r.Slug+" "+c.CheckID] = c.Status
		}
	}
	for key, want := range map[string]models.CheckStatus{
		"acme/open-kit renovate_config":   models.StatusPass,
		"acme/open-kit ops_manifest":      models.StatusFail,
		"acme/secret-lab renovate_config": models.StatusFail,
	} {
		if got[key] != want {
			t.Errorf("%s = %q, want %q", key, got[key], want)
		}
	}

	// A check in the global ignore list runs on no repo, so its directory is
	// not listed.
	opsListed.Store(false)
	if err := os.WriteFile(cfg, []byte("scope:\n  github:\n    type: org\n    name: acme\npolicy:\n  base: "+pol+"\n  ignore: [ops_manifest]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := run(Options{ConfigPath: cfg, Format: "json"}); code >= 2 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	if opsListed.Load() {
		t.Error("ops/ was listed for an ignored check")
	}
}
