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

package sxclifw

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenPinned(t *testing.T) {
	dir := t.TempDir()
	regular := filepath.Join(dir, "box-config.json")
	if err := os.WriteFile(regular, []byte(`{"core":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "evil.json")
	if err := os.WriteFile(outside, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "linked-config.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	r, err := openPinned(regular)
	if err != nil {
		t.Fatalf("regular companion must open: %v", err)
	}
	content, _ := io.ReadAll(r)
	r.Close()
	if string(content) != `{"core":{}}` {
		t.Errorf("content wrong: %q", content)
	}

	if _, err := openPinned(link); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Errorf("symlinked companion must be rejected with a clear message, got %v", err)
	}

	if _, err := openPinned(filepath.Join(dir, "missing.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing companion must report fs.ErrNotExist for the skip logic, got %v", err)
	}
}

func TestRealBinaryDir(t *testing.T) {
	dir, err := realBinaryDir()
	if err != nil || dir == "" {
		t.Fatalf("realBinaryDir failed: %q, %v", dir, err)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil || resolved != dir {
		t.Errorf("returned dir must already be fully resolved: %q vs %q (%v)", dir, resolved, err)
	}
}
