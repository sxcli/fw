package sxclifw

import (
	"os"
	"path/filepath"
)

// realBinaryDir returns the directory of the real binary: the executable
// path with every symlink resolved. Busybox-style applet symlinks must
// never relocate the binary-companion config location — a symlink to the
// binary in an attacker-writable directory would otherwise choose the
// binary's configuration.
func realBinaryDir() (string, error) {
	var dir string
	exe, err := os.Executable()
	if err == nil {
		if exe, err = filepath.EvalSymlinks(exe); err == nil {
			dir = filepath.Dir(exe)
		}
	}
	return dir, err
}
