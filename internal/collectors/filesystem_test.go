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

// A symlink pointing at a directory must not be listed as a file (os.walk treats
// it as a dirname, not a filename); a symlink to a file is kept.
func TestFilesystemSkipsDirSymlinks(t *testing.T) {
	root := t.TempDir()
	mkfile(t, root, "real/inner.txt", "x")
	mkfile(t, root, "target.txt", "y")
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "linkdir")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "target.txt"), filepath.Join(root, "linkfile")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	repo := Filesystem{}.Collect(source.Repo{Type: "local", Slug: root, Path: root})
	got := strings.Join(repo.FS.Files, ",")
	if strings.Contains(got, "linkdir") {
		t.Errorf("directory symlink listed as a file: %s", got)
	}
	if !strings.Contains(got, "linkfile") {
		t.Errorf("file symlink should be listed: %s", got)
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

// unreadable makes dir unreadable for the test and restores it afterwards, so
// t.TempDir can clean up. Root ignores permissions, so the test is skipped.
func unreadable(t *testing.T, dir string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("permissions do not restrict root")
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}

// A directory the walk cannot read is unknown, not empty: it is recorded, with
// the listed evidence directories beneath it, since one walk covers the whole
// tree. A README that is listed but cannot be read is recorded too.
func TestFilesystemRecordsUnreadEvidence(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"README.md", ".github/CODEOWNERS", ".github/workflows/ci.yml", "src/main.go"} {
		p := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("# x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	unreadable(t, filepath.Join(root, ".github"))
	unreadable(t, filepath.Join(root, "README.md"))

	got := Filesystem{}.Collect(source.Repo{Type: "local", Slug: "x", Path: root}).FS
	if want := []string{".github", ".github/workflows"}; strings.Join(got.UnreadDirs, ",") != strings.Join(want, ",") {
		t.Errorf("UnreadDirs = %q, want %q", got.UnreadDirs, want)
	}
	if !got.ReadmeUnread {
		t.Error("ReadmeUnread = false for a README that could not be read")
	}
	if !got.KeyFiles["README"] {
		t.Error("README is still listed; only its content is unknown")
	}
}

// With no README listed and a directory the walk could not read, the README
// may be in that directory, so its content is unread rather than absent.
func TestFilesystemReadmeUnreadWhenItsDirectoryMayBeUnread(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("# x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unreadable(t, src)
	got := Filesystem{}.Collect(source.Repo{Type: "local", Slug: "x", Path: root}).FS
	if !got.ReadmeUnread {
		t.Errorf("ReadmeUnread = false although the only README is in an unreadable directory: %+v", got)
	}
	readable := t.TempDir()
	if got := (Filesystem{}).Collect(source.Repo{Type: "local", Slug: "y", Path: readable}).FS; got.ReadmeUnread {
		t.Error("a fully readable checkout with no README must not be ReadmeUnread")
	}
}
