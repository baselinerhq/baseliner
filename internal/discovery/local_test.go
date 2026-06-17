package discovery

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalDiscover(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "repo")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "afile")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := Local{Paths: []string{sub, file, filepath.Join(dir, "missing")}}.Discover()
	if len(got) != 1 {
		t.Fatalf("got %d sources, want 1 (dir only)", len(got))
	}
	if got[0].Type != "local" {
		t.Errorf("type = %q", got[0].Type)
	}
	resolved, _ := filepath.EvalSymlinks(sub)
	if got[0].Slug != resolved || got[0].Path != resolved {
		t.Errorf("slug/path = %q/%q, want %q", got[0].Slug, got[0].Path, resolved)
	}
}
