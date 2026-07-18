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

package fw

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
