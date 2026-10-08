//go:build unix

package collectors

import (
	"errors"
	"os"
	"syscall"
)

// openRegular opens path for reading only if it is a regular file, without
// following a symlink at path and without blocking if it is a pipe.
func openRegular(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, errors.New("not a regular file")
	}
	return f, nil
}
