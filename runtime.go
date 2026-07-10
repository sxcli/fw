package sxclifw

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/sxcli/sxcli-fw/internal/config"
	"github.com/sxcli/sxcli-fw/internal/fail"
	"github.com/sxcli/sxcli-fw/internal/registry"
)

// runtime carries every external dependency of one run, injectable for
// hermetic tests. Main builds the production one from the package
// globals and the platform layer.
type runtime struct {
	reg        *registry.Registry
	c          *fail.Collector
	argv       []string
	lookupEnv  func(string) (string, bool)
	stdout     io.Writer
	stderr     io.Writer
	locations  func(appletID string) []config.Location
	stat       func(string) (int64, error)
	open       func(string) (io.ReadCloser, error)
	openPinned func(string) (io.ReadCloser, error)
	suppressed []string
	maxConfig  int64            // config file size cap; <=0 → the 1 MiB default
	execApplet func(Applet) int // nil → applet.Run(); the SCM handler overrides
	reported   bool
}

func productionRuntime(argv []string, execApplet func(Applet) int) *runtime {
	return &runtime{
		reg:        defaultRegistry,
		c:          defaultCollector,
		argv:       argv,
		lookupEnv:  os.LookupEnv,
		stdout:     os.Stdout,
		stderr:     os.Stderr,
		locations:  productionLocations,
		stat:       statRegular,
		open:       func(path string) (io.ReadCloser, error) { return os.Open(path) },
		openPinned: openPinned,
		suppressed: suppressedCore,
		maxConfig:  maxConfigSize,
		execApplet: execApplet,
	}
}

// statRegular probes one config source: a config file must resolve to a
// regular file. os.Stat follows symlinks, so a symlink to a regular
// file passes (symlink-overlay distros keep working) — it is the
// resolved target that must be regular. FIFOs are refused here, before
// any open could block on them; devices and directories get a clean
// startup error instead of downstream read weirdness.
func statRegular(path string) (int64, error) {
	var size int64
	fi, err := os.Stat(path)
	if err == nil {
		if fi.Mode().IsRegular() {
			size = fi.Size()
		} else {
			err = fmt.Errorf("not a regular file (%s)", fi.Mode())
		}
	}
	return size, err
}

// productionLocations returns the config search locations of one applet:
// the pinned binary companion, the system location, the user location.
// An unresolvable binary path silently skips the companion; an
// unresolvable user config dir skips the user location.
func productionLocations(appletID string) []config.Location {
	var out []config.Location
	if dir, err := realBinaryDir(); err == nil {
		out = append(out, config.Location{Base: filepath.Join(dir, appletID+"-config"), Pinned: true})
	}
	out = append(out, config.Location{Base: filepath.Join(systemConfigDir(), appletID, "config")})
	if dir, err := os.UserConfigDir(); err == nil {
		out = append(out, config.Location{Base: filepath.Join(dir, appletID, "config")})
	}
	return out
}
