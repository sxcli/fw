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
	"reflect"

	"sxcli.dev/conf/engine"
	"sxcli.dev/conf/fail"
	"sxcli.dev/fw/internal/registry"
)

// defaultCollector accumulates every startup violation across all
// phases; Main reports its content and exits when it is non-empty.
var defaultCollector = &fail.Collector{}

// defaultRegistry is THE catalog: populated by registration chains
// committing from init() or main; composed by the Builder.
var defaultRegistry = registry.New(defaultCollector)

// Registration is a service registration under construction: created
// by NewRegistration or NewBareRegistration, enriched by the chain
// methods, committed to the catalog by the Register terminal —
// construct freely, commit completely. Until Register is called
// nothing exists anywhere; the catalog never holds a half-built entry.
type Registration[T any] struct {
	id        string
	factory   func() *T
	cfgType   reflect.Type // *C, nil for bare registrations
	access    func(*T) any // returns the instance's *C; nil for bare
	aliases   []string     // primary first
	provides  []reflect.Type
	metadata  *Metadata
	steps     []engine.Step
	hidden    bool
	system    bool
	committed bool
}

// NewRegistration starts the registration of a service owning a
// Configuration struct. The id is the service's identity — by
// convention the package's import path (with a /name suffix when one
// package registers several services); sxcli-vet verifies the
// convention, the runtime verifies only the shape. The factory
// constructs the instance — constructors are CHEAP by contract:
// allocate and set the config defaults, nothing else; I/O belongs to
// Configured. cfg is the accessor from the instance to its config
// struct; the type parameters ride this entry function because Go
// permits them nowhere else (methods cannot be generic).
func NewRegistration[T, C any](id string, factory func() *T, cfg func(*T) *C) *Registration[T] {
	r := NewBareRegistration(id, factory)
	r.cfgType = reflect.TypeOf((*C)(nil))
	if cfg != nil {
		r.access = func(inst *T) any { return cfg(inst) }
	}
	return r
}

// NewBareRegistration starts the registration of a service with no
// Configuration struct. Everything else is NewRegistration.
func NewBareRegistration[T any](id string, factory func() *T) *Registration[T] {
	return &Registration[T]{id: id, factory: factory}
}

// Iface returns the type token of an interface, for Provides. The one
// bridge the no-generic-methods rule forces.
func Iface[I any]() reflect.Type {
	return reflect.TypeOf((*I)(nil)).Elem()
}

// Alias declares the service's operator-facing names — REQUIRED, and
// deliberately never derived: the author who names their service made
// a choice they can be blamed for. The first name is primary (env
// prefix, config section, listings); all are selectable. Lowercase,
// digits and hyphens; hyphens reach the environment as underscores.
func (r *Registration[T]) Alias(names ...string) *Registration[T] {
	r.aliases = append(r.aliases, names...)
	return r
}

// Provides declares the interfaces the service provides, as Iface
// tokens. Only declared interfaces participate in dependency
// injection; declaring one the concrete type does not implement is a
// violation at the Register commit.
func (r *Registration[T]) Provides(types ...reflect.Type) *Registration[T] {
	r.provides = append(r.provides, types...)
	return r
}

// Step declares one link of a config migration chain: the typed
// conversion from schema version `from` to the next. Old versions
// live on as plain json-only types:
//
//	fw.Step(1, func(old ConfigV1) ConfigV2 { … })
func Step[From, To any](from uint32, fn func(From) To) engine.Step {
	return engine.NewStep(from, fn)
}

// Migrate attaches the service's config migration chain, oldest step
// first — how a schema evolves without stranding deployed files. The
// chain shape is validated when each invocation's schema is built.
func (r *Registration[T]) Migrate(steps ...engine.Step) *Registration[T] {
	r.steps = append(r.steps, steps...)
	return r
}

// Metadata attaches the service's declarative description.
func (r *Registration[T]) Metadata(md *Metadata) *Registration[T] {
	r.metadata = md
	return r
}

// Hidden marks an applet as a hidden command: selectable by an
// explicit first token, absent from listings and basename dispatch.
func (r *Registration[T]) Hidden() *Registration[T] {
	r.hidden = true
	return r
}

// System marks an applet as machinery of the binary — invoked by
// tooling, never typed by a human. Implies Hidden; excluded from
// single-applet counting.
func (r *Registration[T]) System() *Registration[T] {
	r.system = true
	return r
}

// Register is the terminal: validate the completed registration and
// commit it to the catalog. All typed checks run here — the last
// moment T and C are statically known — and every violation is
// recorded for the all-at-once startup report; nothing panics. The
// second terminal is Solo, which commits and runs.
func (r *Registration[T]) Register() {
	r.registerInto(defaultRegistry, defaultCollector)
}

// registerInto is Register against explicit targets; tests use it with
// private catalogs, exactly as the old public Register delegated.
func (r *Registration[T]) registerInto(reg *registry.Registry, c *fail.Collector) {
	before := c.Len()
	concrete := reflect.TypeOf((*T)(nil))
	if r.committed {
		c.Fail("service %q: registered twice", r.id)
	}
	if !validServiceID(r.id) {
		c.Fail("service id %q: must be path-shaped (lowercase segments of letters, digits, '.', '-', '_')", r.id)
	} else if r.id == CoreID {
		c.Fail("service id %q is reserved for the framework core", r.id)
	}
	if len(r.aliases) == 0 {
		c.Fail("service %q: an alias is required — name what operators will type", r.id)
	}
	seen := map[string]bool{}
	for _, a := range r.aliases {
		if !validAlias(a) {
			c.Fail("service %q: alias %q must be lowercase letters, digits and hyphens, starting with a letter", r.id, a)
		} else if a == CoreAlias || a == IntrospectionAlias {
			c.Fail("service %q: alias %q is reserved", r.id, a)
		} else if seen[a] {
			c.Fail("service %q: alias %q declared twice", r.id, a)
		}
		seen[a] = true
	}
	for _, it := range r.provides {
		if it == nil || it.Kind() != reflect.Interface {
			c.Fail("service %q: Provides takes interface tokens (fw.Iface[I]())", r.id)
		} else if !concrete.Implements(it) {
			c.Fail("service %q: %s does not implement declared interface %s", r.id, concrete, it)
		}
	}
	isApplet := concrete.Implements(appletType)
	if isApplet && concrete.Implements(stopperType) {
		c.Fail("service %q: an applet must not implement Starter or Stopper", r.id)
	}
	if (r.hidden || r.system) && !isApplet {
		c.Fail("service %q: Hidden/System apply only to applets", r.id)
	}
	if r.cfgType != nil && r.access == nil {
		c.Fail("service %q: nil config accessor — use NewBareRegistration for config-less services", r.id)
	}
	if len(r.steps) > 0 && r.cfgType == nil {
		c.Fail("service %q: Migrate requires a config struct — a bare registration has no schema to evolve", r.id)
	}
	if !isApplet && engine.HasPositionals(r.cfgType) {
		c.Fail("service %q: positionals are invocation data — only applet configs may declare pos fields", r.id)
	}
	if r.cfgType != nil {
		// tag and field-type validation, type-level: the registration
		// list promises "malformed tags" at registration, not at the
		// first invocation that happens to plan this service
		if err := engine.ValidateConfigType(r.id, r.cfgType); err != nil {
			c.Add(err)
		}
	}
	var meta any
	if r.metadata != nil {
		normalized, errs := normalizeMetadata(r.id, r.metadata, r.cfgType != nil, engine.ProbeType(r.cfgType), false)
		for _, err := range errs {
			c.Add(err)
		}
		meta = normalized
	}
	if c.Len() == before {
		r.committed = true
		factory, access := r.factory, r.access
		reg.Commit(&registry.Descriptor{
			ID:         r.id,
			Concrete:   concrete,
			Provides:   append([]reflect.Type(nil), r.provides...),
			Metadata:   meta,
			Hidden:     r.hidden || r.system,
			System:     r.system,
			Aliases:    append([]string(nil), r.aliases...),
			CfgType:    r.cfgType,
			Migrations: append([]engine.Step(nil), r.steps...),
			Make: func() (any, any) {
				inst := factory()
				var cfgPtr any
				if access != nil {
					cfgPtr = access(inst)
				}
				return inst, cfgPtr
			},
		})
	}
}

var appletType = reflect.TypeOf((*Applet)(nil)).Elem()
var stopperType = reflect.TypeOf((*Stopper)(nil)).Elem()
