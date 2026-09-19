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

import "golang.org/x/sys/windows/svc"

// SCMApplet is an applet built to run as a Windows service under
// the Service Control Manager. It extends Applet, so the same
// registration still runs as a plain console process when launched
// that way; Main asks Windows which mode the process is in. A
// Windows service process drives Execute; a normal process drives
// Run as usual.
//
// In Windows service mode Main calls svc.Run with a framework-owned
// svc.Handler. That handler:
//
//   - reports start-pending to the SCM immediately, so the Windows
//     service is not killed for a slow startup
//   - receives the argument vector in its own Execute call — that
//     is where args come from in Windows service mode
//   - runs the standard pipeline (parse, resolve, configure, start)
//   - delegates to the applet's Execute, forwarding the SCM
//     request/status channels: the applet MUST handle stop,
//     shutdown and interrogate itself — the framework answers no
//     SCM request
//   - after the applet's Execute returns, stops every started
//     service in reverse order and reports the final status to
//     the SCM
//
// When the applet's Execute is invoked the Windows service state is
// start-pending. From that point the applet's Execute owns the
// Windows service state: it reports Running, with the
// accepted-commands mask it wants, once it is ready to serve — the
// framework never does. Before returning it MUST report
// stop-pending, so the SCM keeps waiting while the framework runs
// its own stop procedure — the reverse-order Stop of every started
// service — before the process exits. Execute's signature is
// svc.Handler's, so the channels carry the SCM's own types.
type SCMApplet interface {
	Applet
	Execute(args []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (svcSpecificEC bool, exitCode uint32)
}
