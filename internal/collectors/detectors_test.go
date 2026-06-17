package collectors

import (
	"reflect"
	"testing"
)

func TestDetectKeyFiles(t *testing.T) {
	files := []string{
		"README.md", "LICENSE", ".gitignore", ".github/CODEOWNERS",
		"docs/CODEOWNERS", // wrong location — must NOT count
	}
	kf := DetectKeyFiles(files)
	for _, k := range []string{"README", "LICENSE", "GITIGNORE", "CODEOWNERS"} {
		if !kf[k] {
			t.Errorf("%s not detected", k)
		}
	}

	// CODEOWNERS only in docs/ must not count.
	kf2 := DetectKeyFiles([]string{"docs/CODEOWNERS"})
	if kf2["CODEOWNERS"] {
		t.Error("CODEOWNERS in docs/ should not count")
	}
	// root CODEOWNERS counts.
	if !DetectKeyFiles([]string{"CODEOWNERS"})["CODEOWNERS"] {
		t.Error("root CODEOWNERS should count")
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
