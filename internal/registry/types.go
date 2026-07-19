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

// Package registry implements the structural service catalog of the
// sxcli framework. It is deliberately ignorant of the framework's
// interfaces: the root package's registration chain runs every semantic
// check in typed land and commits finished descriptors; the registry
// validates only what it owns — id uniqueness and inject-tag structure.
package registry

import (
	"reflect"

	"sxcli.dev/fw/internal/fail"
)

// DepField describes one `inject`-annotated field of a registered
// instance.
type DepField struct {
	Index    []int        // reflect field index, usable with FieldByIndex
	Name     string       // field name, for error messages
	Type     reflect.Type // field type; for slices the element type
	IsSlice  bool
	IDs      []string // service ids from the tag, may be empty
	Optional bool
}

// Descriptor is the registry's record of one registered service.
type Descriptor struct {
	ID        string
	Instance  any
	Concrete  reflect.Type   // the *Struct type of Instance
	Provides  []reflect.Type // declared and verified interfaces
	ConfigPtr any            // nil when the service has no configuration
	Metadata  any            // opaque; normalized by the root's metadata check
	Hidden    bool           // absent from listings and basename dispatch
	System    bool           // binary machinery; ignored by single-applet counting
	Deps      []DepField

	// Catalog-model fields (the composition release). On a committed
	// catalog entry Instance and ConfigPtr stay nil until Build calls
	// Make — the catalog holds factories and declarations, no state.
	Aliases    []string                          // operator-facing names, primary first
	Ranked     bool                              // listed in the composition's Order: entitled to win single-valued ties
	CfgType    reflect.Type                      // *C, nil for config-less services
	Migrations any                               // opaque migration steps ([]engine.Step); the root maps them into sections
	Make       func() (instance any, cfgPtr any) // factory ⊗ accessor, composed
	// in typed land; every static check ran before it was erased
}

// Registry collects service descriptors. It never panics: every
// violation is recorded into the shared startup collector so startup can
// fail listing all problems at once.
type Registry struct {
	c       *fail.Collector
	ordered []*Descriptor
	byID    map[string]*Descriptor
}
