package runner

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baselinerhq/baseliner/internal/models"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e.x",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e.x")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// fullPassingRepo builds a local repo that passes all 10 checks (main branch, fresh).
func fullPassingRepo(t *testing.T) string {
	return buildRepo(t, true)
}

// repoMissingCodeowners passes everything except codeowners_exists (low, weight 1)
// -> score ~0.96 with one failing check.
func repoMissingCodeowners(t *testing.T) string {
	return buildRepo(t, false)
}

func buildRepo(t *testing.T, withCodeowners bool) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "README.md", "# Title\n\nbody")
	writeFile(t, root, "LICENSE", "MIT")
	writeFile(t, root, ".gitignore", "*.tmp")
	if withCodeowners {
		writeFile(t, root, ".github/CODEOWNERS", "* @team")
	}
	writeFile(t, root, ".github/workflows/ci.yml", "on: push")
	writeFile(t, root, ".github/dependabot.yml", "version: 2")
	git(t, root, "init", "-q", "-b", "main")
	git(t, root, "add", "-A")
	git(t, root, "commit", "-qm", "init")
	git(t, root, "update-ref", "refs/remotes/origin/main", "HEAD")
	git(t, root, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	return root
}

func fptr(f float64) *float64 { return &f }

func localConfig(t *testing.T, paths ...string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("scope:\n  local:\n    paths:\n")
	for _, p := range paths {
		b.WriteString("      - " + p + "\n")
	}
	b.WriteString("policy:\n  base: default\n")
	cfg := filepath.Join(t.TempDir(), "baseliner.yaml")
	if err := os.WriteFile(cfg, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func run(opts Options) (int, string, string) {
	var out, errb bytes.Buffer
	code := Scan(&out, &errb, opts)
	return code, out.String(), errb.String()
}

func TestScanAllPassExit0(t *testing.T) {
	cfg := localConfig(t, fullPassingRepo(t))
	code, out, _ := run(Options{ConfigPath: cfg, Format: "both"})
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "1 passed, 0 failed") {
		t.Errorf("footer missing pass:\n%s", out)
	}
}

func TestScanFailuresExit1(t *testing.T) {
	empty := t.TempDir() // no files -> fails checks
	cfg := localConfig(t, empty)
	code, _, _ := run(Options{ConfigPath: cfg, Format: "table"})
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestScanMissingConfigExit2(t *testing.T) {
	code, _, errOut := run(Options{ConfigPath: "/no/such/baseliner.yaml", Format: "both"})
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.HasPrefix(errOut, "Error: Config file not found") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestScanNoSourcesExit2(t *testing.T) {
	cfg := localConfig(t, filepath.Join(t.TempDir(), "missing-dir"))
	code, _, errOut := run(Options{ConfigPath: cfg, Format: "both"})
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errOut, "No repositories discovered") {
		t.Errorf("stderr = %q", errOut)
	}
}

// mergeCollectionErrors appends synthetic error results in order and recomputes
// the pass/fail counts (a collection_error repo counts as failed).
func TestMergeCollectionErrors(t *testing.T) {
	ts := time.Unix(0, 0).UTC()
	run := models.RunResult{
		TotalRepos: 1, Passed: 1, Failed: 0,
		Repos: []models.RepoResult{{Slug: "ok/repo", Timestamp: ts, Score: 1.0}},
	}
	collErrors := []models.RepoResult{
		models.NewErrorResult("bad/repo", ts, "collection_error", "boom"),
	}
	merged := mergeCollectionErrors(run, collErrors)
	if merged.TotalRepos != 2 || merged.Passed != 1 || merged.Failed != 1 {
		t.Fatalf("counts: total=%d passed=%d failed=%d, want 2/1/1", merged.TotalRepos, merged.Passed, merged.Failed)
	}
	if last := merged.Repos[len(merged.Repos)-1]; last.Slug != "bad/repo" || last.Results[0].CheckID != "collection_error" {
		t.Errorf("error result not appended last: %+v", last)
	}
}

func TestScanFailUnderPassesAboveThreshold(t *testing.T) {
	cfg := localConfig(t, fullPassingRepo(t)) // 1.00
	code, _, _ := run(Options{ConfigPath: cfg, Format: "table", FailUnder: fptr(0.8)})
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (1.00 >= 0.8)", code)
	}
}

func TestScanFailUnderFailsBelowThreshold(t *testing.T) {
	cfg := localConfig(t, t.TempDir()) // empty dir -> score 0.0
	code, _, errOut := run(Options{ConfigPath: cfg, Format: "table", FailUnder: fptr(0.8)})
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (0.0 < 0.8)", code)
	}
	if !strings.Contains(errOut, "below --fail-under") {
		t.Errorf("stderr = %q, want a 'below --fail-under' note", errOut)
	}
}

// The defining behavior: --fail-under replaces the per-check gate. A repo with a
// failing check but a score above the threshold must pass.
func TestScanFailUnderToleratesFailingCheck(t *testing.T) {
	cfg := localConfig(t, repoMissingCodeowners(t)) // ~0.96, one failing check
	code, _, _ := run(Options{ConfigPath: cfg, Format: "table", FailUnder: fptr(0.9)})
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (0.96 >= 0.9 despite a failing check)", code)
	}
	// And the same repo fails a stricter threshold.
	code, _, _ = run(Options{ConfigPath: cfg, Format: "table", FailUnder: fptr(0.99)})
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (0.96 < 0.99)", code)
	}
}

func TestScanFailUnderInvalidExit2(t *testing.T) {
	cfg := localConfig(t, fullPassingRepo(t))
	code, _, errOut := run(Options{ConfigPath: cfg, Format: "table", FailUnder: fptr(1.5)})
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errOut, "invalid --fail-under") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestScanWritesSARIF(t *testing.T) {
	cfg := localConfig(t, t.TempDir()) // empty repo -> findings
	sarif := filepath.Join(t.TempDir(), "out.sarif")
	code, _, _ := run(Options{ConfigPath: cfg, Format: "table", SarifFile: sarif})
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	data, err := os.ReadFile(sarif)
	if err != nil {
		t.Fatalf("sarif file: %v", err)
	}
	var doc struct {
		Version string `json:"version"`
		Runs    []struct {
			Results []map[string]any `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("sarif not valid JSON: %v", err)
	}
	if doc.Version != "2.1.0" {
		t.Errorf("sarif version = %q", doc.Version)
	}
	if len(doc.Runs) != 1 || len(doc.Runs[0].Results) == 0 {
		t.Errorf("expected results in the SARIF, got %+v", doc.Runs)
	}
}

func TestScanInvalidFormatExit2(t *testing.T) {
	cfg := localConfig(t, fullPassingRepo(t))
	code, _, errOut := run(Options{ConfigPath: cfg, Format: "yaml"})
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errOut, "invalid --format") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestScanJSONToFile(t *testing.T) {
	cfg := localConfig(t, fullPassingRepo(t))
	outFile := filepath.Join(t.TempDir(), "results.json")
	code, _, _ := run(Options{ConfigPath: cfg, Format: "json", OutputFile: outFile})
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("results file: %v", err)
	}
	if !strings.Contains(string(data), `"total_repos": 1`) {
		t.Errorf("json missing total_repos:\n%s", data)
	}
}
