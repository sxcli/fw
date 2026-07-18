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

// SCMApplet is an applet that can run under the Windows Service Control
// Manager. It extends Applet, so the same applet serves both launch
// modes: started as a normal process the framework drives Run as usual;
// under the SCM it drives Execute instead.
//
// In service mode Main calls svc.Run with a framework-owned handler; that
// handler reports a start-pending status to the SCM immediately — so the
// service is not killed for slow startup — receives the argument vector
// in its Execute call, runs the standard pipeline (parse, resolve,
// configure, start), and only then delegates to the applet's Execute,
// forwarding the SCM request/status channels so stop, shutdown and
// interrogate requests reach the applet directly.
//
// When Execute is invoked the service state reported to the SCM is
// still start-pending: the framework never reports Running — the applet
// owns that transition and performs it itself (with the
// accepted-commands mask it wants) once it is ready to serve.
//
// When Execute returns, the framework stops every started service in
// reverse order and reports the final status to the SCM. The signature
// mirrors svc.Handler's Execute.
type SCMApplet interface {
	Applet
	Execute(args []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (svcSpecificEC bool, exitCode uint32)
}
