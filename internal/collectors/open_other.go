//go:build !unix

package collectors

import (
	"errors"
	"os"
)

// openRegular opens path for reading only if it is a regular file. Without
// O_NOFOLLOW here, the caller's Lstat is what refuses a symlink.
func openRegular(path string) (*os.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, errors.New("not a regular file")
	}
	return f, nil
}
