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

//go:build unix

package fw

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestStatRegular(t *testing.T) {
	dir := t.TempDir()

	regular := filepath.Join(dir, "config.json")
	if err := os.WriteFile(regular, []byte(`{"core":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if size, err := statRegular(regular); err != nil || size != int64(len(`{"core":{}}`)) {
		t.Errorf("regular file must pass with its size: %d, %v", size, err)
	}

	linked := filepath.Join(dir, "linked.json")
	if err := os.Symlink(regular, linked); err != nil {
		t.Fatal(err)
	}
	if _, err := statRegular(linked); err != nil {
		t.Errorf("a symlink resolving to a regular file must pass: %v", err)
	}

	fifo := filepath.Join(dir, "config.fifo.json")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := statRegular(fifo); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("a fifo must be refused without opening it: %v", err)
	}

	if _, err := statRegular(dir); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("a directory must be refused: %v", err)
	}

	if _, err := statRegular(filepath.Join(dir, "missing.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a missing file must report fs.ErrNotExist for the skip logic: %v", err)
	}
}
