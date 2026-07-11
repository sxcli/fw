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

// Package registry implements the structural service registry of the
// sxcli framework. It is deliberately ignorant of the framework's
// interfaces: it validates identity, shape and tags, and stores
// descriptors. Semantic rules are supplied by the root package as Check
// functions run against every descriptor at registration time.
package registry

import (
	"reflect"

	"sxcli.dev/fw/internal/fail"
)

// Check is a semantic validation hook supplied by the framework root. A
// non-nil result is recorded like any other registration violation.
type Check func(d *Descriptor) error

// Options carries the folded result of the root package's RegisterOption
// values for a single Register call.
type Options struct {
	Interfaces []reflect.Type
	Config     any
}

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
	Deps      []DepField
}

// Registry collects service descriptors. It never panics: every
// violation is recorded into the shared startup collector so startup can
// fail listing all problems at once.
type Registry struct {
	c        *fail.Collector
	checks   []Check
	ordered  []*Descriptor
	byID     map[string]*Descriptor
	concrete map[reflect.Type]string // concrete type → id that claimed it
}
