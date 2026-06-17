package collectors

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baselinerhq/baseliner/internal/source"
)

func mkfile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFilesystemCollectFull(t *testing.T) {
	root := t.TempDir()
	mkfile(t, root, "README.md", "# Title\n\nbody")
	mkfile(t, root, "LICENSE", "MIT")
	mkfile(t, root, ".gitignore", "*.tmp")
	mkfile(t, root, ".github/CODEOWNERS", "* @team")
	mkfile(t, root, ".github/workflows/ci.yml", "on: push")
	mkfile(t, root, ".github/dependabot.yml", "version: 2")
	mkfile(t, root, "a/b/c/deep.txt", "ok")     // 4 parts — included
	mkfile(t, root, "a/b/c/d/toodeep.txt", "x") // 5 parts — excluded
	mkfile(t, root, ".git/config", "[core]")    // .git — excluded

	repo := Filesystem{}.Collect(source.Repo{Type: "local", Slug: root, Path: root})
	fs := repo.FS
	for _, k := range []string{"README", "LICENSE", "GITIGNORE", "CODEOWNERS"} {
		if !fs.KeyFiles[k] {
			t.Errorf("%s not detected", k)
		}
	}
	if len(fs.CIFiles) != 1 || len(fs.DepUpdateFiles) != 1 {
		t.Errorf("ci=%v dep=%v", fs.CIFiles, fs.DepUpdateFiles)
	}
	if fs.ReadmeContent == nil || *fs.ReadmeContent != "# Title\n\nbody" {
		t.Errorf("readme = %v", fs.ReadmeContent)
	}
	for _, f := range fs.Files {
		if f == "a/b/c/d/toodeep.txt" {
			t.Error("file deeper than depth 4 should be excluded")
		}
		if strings.HasPrefix(f, ".git/") {
			t.Error(".git file should be excluded")
		}
	}
}

func TestFilesystemMissingPath(t *testing.T) {
	repo := Filesystem{}.Collect(source.Repo{Type: "local", Slug: "x/gone", Path: "/no/such/dir"})
	if repo.FS == nil || repo.FS.ReadmeContent != nil {
		t.Error("missing path should yield empty fs context")
	}
	if repo.FS.KeyFiles["README"] {
		t.Error("no key files on missing path")
	}
}

func TestFilesystemReadmeTruncation(t *testing.T) {
	root := t.TempDir()
	big := make([]byte, 5000)
	for i := range big {
		big[i] = 'a'
	}
	mkfile(t, root, "README.md", string(big))
	repo := Filesystem{}.Collect(source.Repo{Type: "local", Slug: root, Path: root})
	if got := len(*repo.FS.ReadmeContent); got != maxReadmeBytes {
		t.Errorf("readme len = %d, want %d", got, maxReadmeBytes)
	}
}
