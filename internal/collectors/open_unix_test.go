//go:build unix

package collectors

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// openRegular refuses on its own what the Lstat before it refuses, so a file
// swapped for a link or a pipe after that Lstat is still not read, and a pipe
// does not block the scan.
func TestOpenRegularRefusesSwappedFiles(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if f, err := openRegular(target); err != nil {
		t.Fatalf("a regular file was refused: %v", err)
	} else {
		_ = f.Close()
	}

	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if f, err := openRegular(link); err == nil {
		_ = f.Close()
		t.Error("a symlink was followed")
	}

	fifo := filepath.Join(root, "fifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skip("mkfifo unavailable:", err)
	}
	done := make(chan error, 1)
	go func() {
		f, err := openRegular(fifo)
		if err == nil {
			_ = f.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a pipe was opened as a waiver file")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("opening a pipe blocked")
	}
}
