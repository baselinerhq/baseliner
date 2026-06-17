package runner

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "README.md", "# Title\n\nbody")
	writeFile(t, root, "LICENSE", "MIT")
	writeFile(t, root, ".gitignore", "*.tmp")
	writeFile(t, root, ".github/CODEOWNERS", "* @team")
	writeFile(t, root, ".github/workflows/ci.yml", "on: push")
	writeFile(t, root, ".github/dependabot.yml", "version: 2")
	git(t, root, "init", "-q", "-b", "main")
	git(t, root, "add", "-A")
	git(t, root, "commit", "-qm", "init")
	git(t, root, "update-ref", "refs/remotes/origin/main", "HEAD")
	git(t, root, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	return root
}

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
