//go:build !windows

package sxclifw

import (
	"os"
	"path/filepath"
)

// platformMain runs the pipeline with the process arguments; there is
// no service mode on this platform.
func platformMain() int {
	return run(productionRuntime(os.Args, nil))
}

// systemConfigDir returns the system-wide config location root.
func systemConfigDir() string {
	return "/etc"
}

// binaryBasename extracts the applet-selector name from argv[0].
func binaryBasename(argv0 string) string {
	return filepath.Base(argv0)
}
