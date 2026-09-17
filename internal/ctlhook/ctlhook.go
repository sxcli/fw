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

// Package ctlhook is the seam between the framework core and the
// optional controls package: importing sxcli.dev/fw/controls fills
// Registered at init, and a binary that never imports it carries no
// control machinery at all — the linker drops the unlinked package.
package ctlhook

import (
	"sxcli.dev/conf/engine"
	"sxcli.dev/conf/fail"
	"sxcli.dev/fw/internal/graph"
)

// CoreID is the framework core's reserved identity — here so the
// controls package can guard it without importing the root package
// (which would cycle through the root's own tests).
const CoreID = "sxcli.dev/fw"

// Ref is what control translation needs to know about a resolved
// reference — identity and the two guard facts.
type Ref struct {
	ID     string
	Applet bool
	Core   bool
}

// View is the working set as the controls see it: two dictionaries,
// nothing else.
type View interface {
	ByAlias(name string) (Ref, bool)
	ByID(id string) (Ref, bool)
}

// Impl is what the controls package registers.
type Impl struct {
	// New returns a fresh knobs struct for one peek or apply pass —
	// the core contribution carrying the three control fields.
	New func() any
	// Meta carries the knobs' completion hints.
	Meta *engine.Meta
	// Translate turns filled knobs into graph controls, recording
	// violations: unknown references, the applet guard and the
	// core-family guards.
	Translate func(c *fail.Collector, view View, knobs any) graph.Controls
	// FeatureLongs maps the controls' CoreFeature values (as ints —
	// the type lives in the root package this seam must not import)
	// to their long argument names, so Suppress can trim them.
	FeatureLongs map[int]string
}

// Registered is nil unless sxcli.dev/fw/controls was imported.
var Registered *Impl
