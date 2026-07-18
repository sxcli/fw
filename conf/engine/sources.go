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

// CompanionLocation returns the pinned binary-companion location, or
// false when the real binary path cannot be resolved.
func CompanionLocation(name string) (Location, bool) {
	if dir, err := realBinaryDir(); err == nil {
		return Location{Base: filepath.Join(dir, name+"-config"), Pinned: true}, true
	}
	return Location{}, false
}

// SystemLocation returns the system-wide location (/etc on unix,
// %ProgramData% on windows).
func SystemLocation(name string) Location {
	return Location{Base: filepath.Join(systemConfigDir(), name, "config")}
}

// UserLocation returns the per-user location (the XDG config dir), or
// false when it cannot be resolved.
func UserLocation(name string) (Location, bool) {
	if dir, err := os.UserConfigDir(); err == nil {
		return Location{Base: filepath.Join(dir, name, "config")}, true
	}
	return Location{}, false
}

// ProductionLocations returns the full config search of one name:
// companion, system, user, in merge order. Callers with tier policy
// (the front door's Suppress) compose the tier constructors instead.
func ProductionLocations(name string) []Location {
	var out []Location
	if loc, ok := CompanionLocation(name); ok {
		out = append(out, loc)
	}
	out = append(out, SystemLocation(name))
	if loc, ok := UserLocation(name); ok {
		out = append(out, loc)
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
