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
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
)

// platformMain detects service mode: under the SCM it hands control to
// svc.Run with the framework handler; with the enabled --scm-debug
// argument it runs the same handler under svc/debug outside the service
// manager (console process, Ctrl+C/Break translated to Stop/Shutdown);
// started normally it runs the pipeline with the process arguments.
// An un-enabled --scm-debug falls through to the normal path where the
// strict parse rejects it as an unknown argument.
func platformMain(app *App) int {
	var code int
	isService, err := svc.IsWindowsService()
	debugArgv, debugMode := stripSCMDebug(os.Args)
	if err == nil && isService {
		h := &scmHandler{app: app}
		runErr := svc.Run("", h)
		code = int(h.code)
		if runErr != nil && code == 0 {
			code = 2
		}
	} else if debugMode && scmDebugEnabled {
		// debug.Run reports a non-zero Execute exit code AS an error
		// (syscall.Errno); the handler holds the real code, so the
		// error only matters when no code was produced at all
		h := &scmHandler{app: app, argv: debugArgv}
		runErr := debug.Run(BinaryBasename(debugArgv[0]), h)
		code = int(h.code)
		if runErr != nil && code == 0 {
			code = 2
		}
	} else {
		code = run(productionRuntime(app, os.Args, nil))
	}
	return code
}

// scmHandler is the framework's svc.Handler for Windows service
// mode: report start-pending to the SCM, receive the argument
// vector in Execute, run the standard pipeline, delegate to the
// applet's SCMApplet.Execute, and after that returns run the
// pipeline's reverse Stop and report the final status to the SCM.
// The applet's side of the contract owns the Windows service state
// from its Execute's invocation, answers every SCM request itself,
// and MUST report stop-pending before returning; this is documented
// on SCMApplet.
type scmHandler struct {
	app      *App
	argv     []string // preset by the --scm-debug path; the SCM path uses Execute's vector
	specific bool
	code     uint32
}

func (h *scmHandler) Execute(args []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	argv := args
	if h.argv != nil {
		argv = h.argv
	}
	status <- svc.Status{State: svc.StartPending}
	rt := productionRuntime(h.app, argv, func(applet Applet) int {
		var out int
		if scmApplet, ok := applet.(SCMApplet); ok {
			h.specific, h.code = scmApplet.Execute(argv, req, status)
			out = int(h.code)
		} else {
			slog.Error("applet does not implement SCMApplet and cannot run as a Windows service")
			h.code = 2
			out = 2
		}
		return out
	})
	if code := run(rt); h.code == 0 && code != 0 {
		h.code = uint32(code)
	}
	status <- svc.Status{State: svc.Stopped}
	return h.specific, h.code
}

// BinaryBasename extracts the applet-selector name from argv[0],
// dropping the .exe suffix — the ONE spelling of the dispatch rule,
// exported for consumers that must agree with it (a completion script
// registered under any other name would answer for a selector
// dispatch refuses).
func BinaryBasename(argv0 string) string {
	return strings.TrimSuffix(filepath.Base(argv0), ".exe")
}

// isSuperuser reports whether the process runs with superuser
// privileges: an elevated token. Windows folks cope with the unix
// name.
func isSuperuser() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}
