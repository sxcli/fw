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
	"sxcli.dev/rules/registration"
	"sxcli.dev/rules/solver"

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
	isCore    bool
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

// core marks the framework's own family. Unexported ON PURPOSE: the
// flag cannot be forged from outside the package (an id-prefix rule
// could be squatted; a chain method could be called) — only fw's own
// init can mint a core member.
func (r *Registration[T]) core() *Registration[T] {
	r.isCore = true
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
// live on as plain json-only types. The converter receives what was
// really set in old (read through has) and declares what it set in
// out (through set); the engine allocates out and anchors both
// presences:
//
//	fw.Step(1, func(old *ConfigV1, has *fw.Presence, out *ConfigV2, set *fw.Presence) {
//		out.Var2 = old.Key
//		set.Add(&out.Var2)
//	})
func Step[From, To any](from uint32, fn func(old *From, has *Presence, out *To, set *Presence)) engine.Step {
	return engine.NewStep(from, fn)
}

// Presence is one config instance's set of present leaves — the
// dimension migration converters read and declare; see the conf
// engine.
type Presence = engine.Presence

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
	// the chain's declarations are judged by the shared rules — the
	// same Check sxcli-vet runs; this side only translates and
	// prefixes
	isApplet := concrete.Implements(appletType)
	chain := registration.Chain{
		ID:           r.id,
		ReservedIDs:  []string{CoreID},
		Aliases:      r.aliases,
		Applet:       isApplet,
		AppletKnown:  true, // reflect always answers
		Starter:      concrete.Implements(starterType),
		Stopper:      concrete.Implements(stopperType),
		Hidden:       r.hidden,
		System:       r.system,
		Core:         r.isCore,
		Reserved:     []string{CoreAlias, SystemAlias},
		HasConfig:    r.cfgType != nil,
		NilAccessor:  r.cfgType != nil && r.access == nil,
		Positionals:  engine.HasPositionals(r.cfgType),
		UpgradeSteps: len(r.steps),
	}
	violations := registration.Check(chain)
	for _, v := range violations {
		c.Fail("service %q: %s", r.id, v.Body)
	}
	// the flow and the words are the shared rules'; only the reflect
	// operations are ours (functions at key places)
	providesBodies := solver.CheckProvides(r.provides,
		func(it reflect.Type) bool { return it != nil && it.Kind() == reflect.Interface },
		func(it reflect.Type) bool { return concrete.Implements(it) },
		func(it reflect.Type) string { return it.String() },
		concrete.String())
	for _, body := range providesBodies {
		c.Fail("service %q: %s", r.id, body)
	}
	if len(r.steps) == 0 || r.cfgType != nil {
		// the chain's SHAPE is type-level work and the commit owns it
		// (spec: "the commit validates the chain"); the engine hands
		// back bare bodies and the prefix is OURS — fw says "service".
		// The steps-without-config case was judged by the shared rules
		// above. The factory-default version, an instance fact, stays
		// schema-time.
		for _, body := range engine.ChainShape(r.steps, r.cfgType) {
			c.Fail("service %q: %s", r.id, body)
		}
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
			Core:       r.isCore,
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
var starterType = reflect.TypeOf((*Starter)(nil)).Elem()
var stopperType = reflect.TypeOf((*Stopper)(nil)).Elem()
