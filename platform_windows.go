package sxclifw

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

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
func platformMain() int {
	var code int
	isService, err := svc.IsWindowsService()
	debugArgv, debugMode := stripSCMDebug(os.Args)
	if err == nil && isService {
		h := &scmHandler{}
		if runErr := svc.Run("", h); runErr == nil {
			code = int(h.code)
		} else {
			code = 2
		}
	} else if debugMode && scmDebugEnabled {
		h := &scmHandler{argv: debugArgv}
		if runErr := debug.Run(binaryBasename(debugArgv[0]), h); runErr == nil {
			code = int(h.code)
		} else {
			code = 2
		}
	} else {
		code = run(productionRuntime(os.Args, nil))
	}
	return code
}

// scmHandler is the framework's svc.Handler: it reports start-pending
// immediately — so the SCM does not kill the service during
// initialization — receives the argument vector in Execute, runs the
// standard pipeline and delegates to the applet's SCMApplet.Execute,
// forwarding the SCM channels. The applet owns the transition to
// Running. After it returns, the pipeline's reverse Stop runs and the
// final status goes to the SCM.
type scmHandler struct {
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
	rt := productionRuntime(argv, func(applet Applet) int {
		var out int
		if scmApplet, ok := applet.(SCMApplet); ok {
			h.specific, h.code = scmApplet.Execute(argv, req, status)
			out = int(h.code)
		} else {
			slog.Error("applet does not implement SCMApplet and cannot run as a service")
			h.code = 2
			out = 2
		}
		return out
	})
	if code := run(rt); h.code == 0 && code != 0 {
		h.code = uint32(code)
	}
	status <- svc.Status{State: svc.StopPending}
	return h.specific, h.code
}

// systemConfigDir returns the system-wide config location root.
func systemConfigDir() string {
	out := os.Getenv("ProgramData")
	if out == "" {
		out = `C:\ProgramData`
	}
	return out
}

// binaryBasename extracts the applet-selector name from argv[0],
// dropping the .exe suffix.
func binaryBasename(argv0 string) string {
	return strings.TrimSuffix(filepath.Base(argv0), ".exe")
}
