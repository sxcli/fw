// Copyright 2026 Plamen K. Kosseff
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package engine

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// StatRegular probes one config source: a config file must resolve to a
// regular file. os.Stat follows symlinks, so a symlink to a regular
// file passes (symlink-overlay distros keep working) — it is the
// resolved target that must be regular. FIFOs are refused here, before
// any open could block on them; devices and directories get a clean
// startup error instead of downstream read weirdness.
func StatRegular(path string) (int64, error) {
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

// ProductionLocations returns the config search locations of one name:
// the pinned binary companion, the system location, the user location.
// An unresolvable binary path silently skips the companion; an
// unresolvable user config dir skips the user location.
func ProductionLocations(name string) []Location {
	var out []Location
	if dir, err := realBinaryDir(); err == nil {
		out = append(out, Location{Base: filepath.Join(dir, name+"-config"), Pinned: true})
	}
	out = append(out, Location{Base: filepath.Join(systemConfigDir(), name, "config")})
	if dir, err := os.UserConfigDir(); err == nil {
		out = append(out, Location{Base: filepath.Join(dir, name, "config")})
	}
	return out
}

// ProductionSources assembles the real-world Sources of one name: os
// argument vector and environment, the standard search locations with
// the hardening stack (regular files only, pinned symlink refusal).
func ProductionSources(name string) Sources {
	return Sources{
		Args:      os.Args[1:],
		LookupEnv: os.LookupEnv,
		Locations: ProductionLocations(name),
		Stat:      StatRegular,
		Lstat: func(path string) error {
			_, err := os.Lstat(path)
			return err
		},
		Open:       func(path string) (io.ReadCloser, error) { return os.Open(path) },
		OpenPinned: OpenPinned,
	}
}
