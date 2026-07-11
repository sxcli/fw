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

// Wine-based SCM tests: cross-compile testdata/scmbox for windows and
// drive its service path under wine via --scm-debug (svc/debug). Skipped
// without wine on PATH or with -short.
package sxclifw_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var wineWorld struct {
	once   sync.Once
	dir    string // shared: the built exe and the wine prefix
	wine   string
	exe    string
	broken string
}

// wineCleanup is called from TestMain after the run.
func wineCleanup() {
	if wineWorld.dir != "" {
		os.RemoveAll(wineWorld.dir)
	}
}

func wineSetup(t *testing.T) (string, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("wine tests are skipped in -short mode")
	}
	wineWorld.once.Do(func() {
		wine, err := exec.LookPath("wine")
		if err != nil {
			if wine, err = exec.LookPath("wine64"); err != nil {
				wineWorld.broken = "wine is not installed"
			}
		}
		if wineWorld.broken == "" {
			dir, terr := os.MkdirTemp("", "sxcli-wine-")
			if terr != nil {
				wineWorld.broken = terr.Error()
			} else {
				exe := filepath.Join(dir, "scmbox.exe")
				build := exec.Command("go", "build", "-o", exe, "./testdata/scmbox")
				build.Env = append(os.Environ(), "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0")
				if out, berr := build.CombinedOutput(); berr != nil {
					wineWorld.broken = "cannot cross-compile scmbox: " + berr.Error() + "\n" + string(out)
				} else {
					wineWorld.dir = dir
					wineWorld.wine = wine
					wineWorld.exe = exe
				}
			}
		}
	})
	if wineWorld.broken != "" {
		t.Skip(wineWorld.broken)
	}
	return wineWorld.wine, wineWorld.exe
}

func winebox(t *testing.T, env map[string]string, args ...string) result {
	t.Helper()
	wine, exe := wineSetup(t)
	cmd := exec.Command(wine, append([]string{exe}, args...)...)
	cmd.Env = append(os.Environ(),
		"WINEPREFIX="+filepath.Join(wineWorld.dir, "prefix"),
		"WINEDEBUG=-all",
	)
	for name, value := range env {
		cmd.Env = append(cmd.Env, name+"="+value)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		t.Fatalf("cannot run wine: %v", err)
	}
	return result{code: cmd.ProcessState.ExitCode(), stdout: stdout.String(), stderr: stderr.String()}
}

func TestWineSCMDebugRunsServicePath(t *testing.T) {
	r := winebox(t, nil, "--scm-debug", "--note", "fromwine", "--exit", "5")
	if r.code != 5 {
		t.Fatalf("exit = %d, want 5\nstdout:\n%s\nstderr:\n%s", r.code, r.stdout, r.stderr)
	}
	if !strings.Contains(r.stdout, "probe: configured note=fromwine") {
		t.Errorf("pipeline must configure inside Execute:\n%s", r.stdout)
	}
	if !strings.Contains(r.stdout, "probe: service execute note=fromwine") {
		t.Errorf("service launch mode must delegate to Execute:\n%s", r.stdout)
	}
	if strings.Contains(r.stdout, "console run") {
		t.Errorf("Run must not be used in service mode:\n%s", r.stdout)
	}
}

func TestWineConsoleModeUsesRun(t *testing.T) {
	r := winebox(t, nil, "--note", "dual")
	if r.code != 0 {
		t.Fatalf("exit = %d\nstdout:\n%s\nstderr:\n%s", r.code, r.stdout, r.stderr)
	}
	if !strings.Contains(r.stdout, "probe: console run note=dual") {
		t.Errorf("console launch mode must use Run:\n%s", r.stdout)
	}
}

func TestWineSCMDebugOffIsUnknownArgument(t *testing.T) {
	r := winebox(t, map[string]string{"SCMBOX_DEBUG_OFF": "1"}, "--scm-debug")
	if r.code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", r.code, r.stdout, r.stderr)
	}
	if !strings.Contains(r.stderr, "unknown argument --scm-debug") {
		t.Errorf("un-enabled --scm-debug must be an unknown argument:\n%s", r.stderr)
	}
}
