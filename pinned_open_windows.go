package sxclifw

import (
	"fmt"
	"io"
	"os"
)

// openPinned opens a binary-companion config file refusing anything that
// is not a regular file — symlinks, junctions and every other reparse
// point or special file. The check is positive (IsRegular) because since
// Go 1.23 Lstat reports junctions/mount points as ModeIrregular, NOT as
// ModeSymlink; a negative symlink-only check would wave a junction
// through. Unlike the unix O_NOFOLLOW variant this is check-then-open;
// the small race is accepted on Windows.
func openPinned(path string) (io.ReadCloser, error) {
	var rc io.ReadCloser
	fi, err := os.Lstat(path)
	if err == nil {
		if fi.Mode().IsRegular() {
			var f *os.File
			if f, err = os.Open(path); err == nil {
				rc = f
			}
		} else {
			err = fmt.Errorf("%s is not a regular file (symlink, junction or other special file): a binary-companion config must be a regular file next to the real binary", path)
		}
	}
	return rc, err
}
