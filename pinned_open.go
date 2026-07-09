//go:build !windows

package sxclifw

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
)

// openPinned opens a binary-companion config file refusing a symlink at
// the final path component (O_NOFOLLOW, enforced atomically by the
// kernel — no check-then-open race): the companion must be a regular
// file that really lives next to the real binary.
func openPinned(path string) (io.ReadCloser, error) {
	var rc io.ReadCloser
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err == nil {
		rc = f
	} else if errors.Is(err, syscall.ELOOP) {
		err = fmt.Errorf("%s is a symlink: a binary-companion config must be a regular file next to the real binary", path)
	}
	return rc, err
}
