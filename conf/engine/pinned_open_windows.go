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
)

// OpenPinned opens a binary-companion config file refusing anything that
// is not a regular file — symlinks, junctions and every other reparse
// point or special file. The check is positive (IsRegular) because since
// Go 1.23 Lstat reports junctions/mount points as ModeIrregular, NOT as
// ModeSymlink; a negative symlink-only check would wave a junction
// through. Unlike the unix O_NOFOLLOW variant this is check-then-open;
// the small race is accepted on Windows.
func OpenPinned(path string) (io.ReadCloser, error) {
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
