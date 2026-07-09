package sxclifw

import "golang.org/x/sys/windows/svc"

// SCMApplet is an applet that runs under the Windows Service Control
// Manager. When Main detects service mode it calls svc.Run with a
// framework-owned handler; that handler reports a start-pending status to
// the SCM immediately — so the service is not killed for slow startup —
// receives the argument vector in its Execute call, runs the standard
// pipeline (parse, resolve, configure, start), and only then delegates to
// the applet's Execute, forwarding the SCM request/status channels so
// stop, shutdown and interrogate requests reach the applet directly.
//
// When Execute is invoked the service state reported to the SCM is
// TODO: document the exact state once the implementation is finished.
//
// When Execute returns, the framework stops every started service in
// reverse order and reports the final status to the SCM. The signature
// mirrors svc.Handler's Execute.
type SCMApplet interface {
	Configurable
	Execute(args []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (svcSpecificEC bool, exitCode uint32)
}
