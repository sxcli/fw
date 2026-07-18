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

//go:build !windows

package engine

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
)

// OpenPinned opens a binary-companion config file refusing a symlink at
// the final path component (O_NOFOLLOW, enforced atomically by the
// kernel — no check-then-open race): the companion must be a regular
// file that really lives next to the real binary.
func OpenPinned(path string) (io.ReadCloser, error) {
	var rc io.ReadCloser
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err == nil {
		rc = f
	} else if errors.Is(err, syscall.ELOOP) {
		err = fmt.Errorf("%s is a symlink: a binary-companion config must be a regular file next to the real binary", path)
	}
	return rc, err
}
