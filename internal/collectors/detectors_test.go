package collectors

import (
	"reflect"
	"testing"
)

func TestDetectKeyFiles(t *testing.T) {
	files := []string{
		"README.md", "LICENSE", ".gitignore", ".github/CODEOWNERS",
	}
	kf := DetectKeyFiles(files)
	for _, k := range []string{"README", "LICENSE", "GITIGNORE", "CODEOWNERS"} {
		if !kf[k] {
			t.Errorf("%s not detected", k)
		}
	}

	// CODEOWNERS is valid in root, .github/, or docs/ (GitHub's three locations).
	for _, loc := range []string{"CODEOWNERS", ".github/CODEOWNERS", "docs/CODEOWNERS"} {
		if !DetectKeyFiles([]string{loc})["CODEOWNERS"] {
			t.Errorf("CODEOWNERS at %q should count", loc)
		}
	}
	// CODEOWNERS in any other directory must not count.
	if DetectKeyFiles([]string{"src/CODEOWNERS"})["CODEOWNERS"] {
		t.Error("CODEOWNERS in src/ should not count")
	}
}

func TestDetectCIFiles(t *testing.T) {
	got := DetectCIFiles([]string{
		".github/workflows/ci.yml", ".github/workflows/release.yaml",
		"Jenkinsfile", ".gitlab-ci.yml", ".circleci/config.yml",
		"README.md", ".github/dependabot.yml",
	})
	want := []string{
		".circleci/config.yml", ".github/workflows/ci.yml",
		".github/workflows/release.yaml", ".gitlab-ci.yml", "Jenkinsfile",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ci files = %v, want %v", got, want)
	}
}

func TestDetectDependencyUpdateFiles(t *testing.T) {
	got := DetectDependencyUpdateFiles([]string{".github/dependabot.yml", "renovate.json", "README.md"})
	want := []string{".github/dependabot.yml", "renovate.json"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dep files = %v, want %v", got, want)
	}
}

func TestFindReadmePath(t *testing.T) {
	if got := FindReadmePath([]string{"src/x.go", "README.rst"}); got != "README.rst" {
		t.Errorf("got %q", got)
	}
	if got := FindReadmePath([]string{"src/x.go"}); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
