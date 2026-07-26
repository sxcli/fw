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
// here, wired to the running composition through the unexported rt
// field — no public seam exists to replace the framework's guts.
// The runtime attaches rt at startup; until then the facilities are
// not usable (nothing can inject the service before a run either).
type systemService struct {
	rt *runtime
}

// the private implementation IS the declared interface.
var _ system.System = (*systemService)(nil)

// Introspector returns the composition's introspection facility.
func (s *systemService) Introspector() system.Introspector {
	return &Introspector{rt: s.rt}
}

// the system service is cataloged like every service — by init,
// through the ordinary chain. fw's own init registering fw's own
// service is the same right every package has; presence in every
// binary follows from the one import that makes a binary an fw
// binary.
func init() {
	NewBareRegistration(system.ID, func() *systemService { return &systemService{} }).
		Alias("system").
		Provides(Iface[system.System]()).
		core().
		Register()
}
