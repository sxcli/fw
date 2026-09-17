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

package registry

import (
	"reflect"

	"sxcli.dev/rules/grammar"
	"sxcli.dev/rules/registration"

	"sxcli.dev/conf/fail"
)

// New creates an empty registry recording violations into c.
func New(c *fail.Collector) *Registry {
	return &Registry{
		c:    c,
		byID: map[string]*Descriptor{},
	}
}

// fail records a registration violation.
func (r *Registry) fail(format string, args ...any) {
	r.c.Fail(format, args...)
}

// Snapshot returns a shallow copy of the registry — same descriptors,
// independent membership — so a later Retain on the original cannot
// shrink the copy. Data-plane only: the snapshot serves reads and
// solves; committing into it is not its purpose.
func (r *Registry) Snapshot() *Registry {
	byID := make(map[string]*Descriptor, len(r.byID))
	for id, d := range r.byID {
		byID[id] = d
	}
	return &Registry{
		c:       r.c,
		byID:    byID,
		ordered: append([]*Descriptor(nil), r.ordered...),
	}
}

// Commit stores a catalog entry built by the root's registration
// chain: the typed side already ran the semantic checks, so the
// registry validates only what it owns — id uniqueness across the
// whole catalog (two packages claiming one id is wrong before any
// composition exists) — and collects the dependency fields from the
// concrete type. A descriptor whose dependencies are already collected
// is taken as-is: Build re-commits catalog copies, and re-reading the
// tags would both double-report tag violations and discard adjustments.
// The same concrete type MAY be cataloged twice; only
// accepting both into one composition is a violation, and that is
// Build's check. Instance stays nil until Build calls Make.
func (r *Registry) Commit(d *Descriptor) {
	if _, dup := r.byID[d.ID]; !dup {
		if d.Deps == nil {
			collectDeps(d, r.c)
		}
		r.ordered = append(r.ordered, d)
		r.byID[d.ID] = d
	} else {
		// BACKSTOP, not the check: the registration chain already
		// judges this via registration.Chain.IDClaimed
		// (sxcli.dev/rules/registration.Check), so a duplicate never
		// reaches Commit through the chain. This branch guards the
		// map against internal callers only, speaking the same rule.
		r.fail(registration.IDInUseRule, d.ID)
	}
}

// Virtual builds a descriptor through the registry's structural
// machinery — dependency collection included — WITHOUT storing it: no
// id claim, no concrete-type claim, no semantic checks. The resolver
// takes such a descriptor as the root of a resolution (the framework
// core's per-invocation node); it never appears in ByID or All.
// Violations (malformed inject tags on the composed struct) are
// recorded like any other — they are framework bugs, not user errors,
// but silence is never the answer.
func (r *Registry) Virtual(id string, instance any, c *fail.Collector) *Descriptor {
	var d *Descriptor
	t := reflect.TypeOf(instance)
	if instance != nil && t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Struct && !reflect.ValueOf(instance).IsNil() {
		d = &Descriptor{ID: id, Instance: instance, Concrete: t}
		// the caller's collector rides as a PARAMETER: Virtual runs
		// against shared snapshots, possibly concurrently — the
		// registry itself is never mutated
		collectDeps(d, c)
	} else {
		c.Fail("virtual service %q: instance must be a non-nil pointer to struct", id)
	}
	return d
}

// ByID returns the descriptor registered under id.
func (r *Registry) ByID(id string) (*Descriptor, bool) {
	d, ok := r.byID[id]
	return d, ok
}

// All returns every stored descriptor in registration order. The order
// is semantic: single-valued dependencies take the first match and slice
// dependencies preserve it.
func (r *Registry) All() []*Descriptor {
	return r.ordered
}

// Retain drops every descriptor whose id is not in keep, so the
// instances of services outside the resolved service set can be
// garbage
// collected (best effort: a package-level reference kept by the
// registering package defeats it). The composition is fixed once
// resolved — ejected services cannot come back.
func (r *Registry) Retain(keep map[string]bool) {
	var kept []*Descriptor
	for _, d := range r.ordered {
		if keep[d.ID] {
			kept = append(kept, d)
		} else {
			delete(r.byID, d.ID)
		}
	}
	r.ordered = kept
}

func collectDeps(d *Descriptor, c *fail.Collector) {
	for _, f := range reflect.VisibleFields(d.Concrete.Elem()) {
		if tag, tagged := f.Tag.Lookup("inject"); tagged {
			if f.IsExported() {
				if ids, optional, err := grammar.ParseInjectTag(tag); err == nil {
					dep := DepField{Index: f.Index, Name: f.Name, IDs: ids, Optional: optional}
					if f.Type.Kind() == reflect.Slice {
						if f.Type.Elem().Kind() == reflect.Interface {
							dep.IsSlice = true
							dep.Type = f.Type.Elem()
							d.Deps = append(d.Deps, dep)
						} else {
							c.Fail("service %q field %s: inject slices carry interfaces only (concrete types are unique)", d.ID, f.Name)
						}
					} else if f.Type.Kind() == reflect.Interface || f.Type.Kind() == reflect.Pointer && f.Type.Elem().Kind() == reflect.Struct {
						if len(ids) <= 1 {
							dep.Type = f.Type
							d.Deps = append(d.Deps, dep)
						} else {
							c.Fail("service %q field %s: a single-valued inject field may name at most one id", d.ID, f.Name)
						}
					} else {
						c.Fail("service %q field %s: inject fields must be an interface, a pointer to struct, or a slice of interface", d.ID, f.Name)
					}
				} else {
					c.Fail("service %q field %s: %v", d.ID, f.Name, err)
				}
			} else {
				c.Fail("service %q field %s: inject tag on unexported field", d.ID, f.Name)
			}
		}
	}
}
