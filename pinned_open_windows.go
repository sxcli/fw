package sxclifw

import (
	"fmt"
	"io"
	"os"
)

// openPinned opens a binary-companion config file refusing reparse
// points (symlinks, junctions): the companion must be a regular file
// that really lives next to the real binary. Unlike the unix O_NOFOLLOW
// variant this is check-then-open; the small race is accepted on
// Windows.
func openPinned(path string) (io.ReadCloser, error) {
	var rc io.ReadCloser
	fi, err := os.Lstat(path)
	if err == nil {
		if fi.Mode()&os.ModeSymlink == 0 {
			var f *os.File
			if f, err = os.Open(path); err == nil {
				rc = f
			}
		} else {
			err = fmt.Errorf("%s is a symlink: a binary-companion config must be a regular file next to the real binary", path)
		}
	}
	return rc, err
}
