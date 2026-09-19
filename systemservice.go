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
	"sxcli.dev/fw/system"
)

// systemService is the framework's own service — the private
// implementation of system.System. PRIVATE is the point: the
// interface lives in the declarations package, the implementation
// here, wired to the running composition through unexported fields —
// no public seam exists to replace the framework's guts. The runtime
// attaches the catalog snapshot at startup, BEFORE ejection: the
// snapshot serves every later view, so completion's own resolved
// service set
// ejects like any other and still answers about the whole binary.
type systemService struct {
	cat *catalog // attach-time snapshot; data-plane only
}

// the private implementation IS the declared interface.
var _ system.System = (*systemService)(nil)

// Introspector returns the target-scoped introspection view for the
// applet the dispatch NAME names (never an id); "" is the binary
// view; an unknown name, a name that is not an applet, or a System
// applet is nil.
// Every view is built from the catalog snapshot and NOTHING else: no
// config files, no location search, no environment — a completion
// query runs per keystroke inside the operator's interactive shell,
// and that shell is not introspection's input. Same binary, same
// target, same answer, always.
func (s *systemService) Introspector(applet string) system.Introspector {
	if s.cat == nil {
		return nil // no run attached: no composition to introspect
	}
	if v := s.cat.introspector(applet); v != nil {
		return v
	}
	return nil // explicit: a typed nil must not masquerade as a view
}

// the system service is cataloged like every service — by init,
// through the ordinary chain. fw's own init registering fw's own
// service is the same right every package has; presence in every
// binary follows from the one import that makes a binary an fw
// binary.
func init() {
	NewBareRegistration(system.ID, func() *systemService { return &systemService{} }).
		Alias(SystemAlias).
		Provides(Iface[system.System]()).
		core().
		Register()
}
