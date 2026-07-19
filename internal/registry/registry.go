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
	"fmt"
	"reflect"
	"strings"

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
			r.collectDeps(d)
		}
		r.ordered = append(r.ordered, d)
		r.byID[d.ID] = d
	} else {
		r.fail("service %q: duplicate id", d.ID)
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
		saved := r.c
		r.c = c
		r.collectDeps(d)
		r.c = saved
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
// instances of services outside the resolved closure can be garbage
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

func (r *Registry) collectDeps(d *Descriptor) {
	for _, f := range reflect.VisibleFields(d.Concrete.Elem()) {
		if tag, tagged := f.Tag.Lookup("inject"); tagged {
			if f.IsExported() {
				if ids, optional, err := parseInjectTag(tag); err == nil {
					dep := DepField{Index: f.Index, Name: f.Name, IDs: ids, Optional: optional}
					if f.Type.Kind() == reflect.Slice {
						if f.Type.Elem().Kind() == reflect.Interface {
							dep.IsSlice = true
							dep.Type = f.Type.Elem()
							d.Deps = append(d.Deps, dep)
						} else {
							r.fail("service %q field %s: inject slices carry interfaces only (concrete types are unique)", d.ID, f.Name)
						}
					} else if f.Type.Kind() == reflect.Interface || f.Type.Kind() == reflect.Pointer && f.Type.Elem().Kind() == reflect.Struct {
						if len(ids) <= 1 {
							dep.Type = f.Type
							d.Deps = append(d.Deps, dep)
						} else {
							r.fail("service %q field %s: a single-valued inject field may name at most one id", d.ID, f.Name)
						}
					} else {
						r.fail("service %q field %s: inject fields must be an interface, a pointer to struct, or a slice of interface", d.ID, f.Name)
					}
				} else {
					r.fail("service %q field %s: %v", d.ID, f.Name, err)
				}
			} else {
				r.fail("service %q field %s: inject tag on unexported field", d.ID, f.Name)
			}
		}
	}
}

// parseInjectTag parses the `inject` tag grammar
// "<id>[,<id>...][;optional]".
func parseInjectTag(tag string) ([]string, bool, error) {
	var ids []string
	var optional bool
	var err error
	idPart := tag
	if i := strings.IndexByte(tag, ';'); i >= 0 {
		idPart = tag[:i]
		if flag := tag[i+1:]; flag == "optional" {
			optional = true
		} else {
			err = fmt.Errorf("unknown inject flag %q", flag)
		}
	}
	if err == nil && idPart != "" {
		for _, raw := range strings.Split(idPart, ",") {
			id := strings.TrimSpace(raw)
			if isValidID(id) {
				ids = append(ids, id)
			} else if err == nil {
				err = fmt.Errorf("invalid service id %q in inject tag", id)
			}
		}
	}
	return ids, optional, err
}

// isValidID reports whether id is a non-empty, all-lowercase go
// identifier (the blank identifier "_" is not a service id).
func isValidID(id string) bool {
	valid := id != "" && id != "_"
	for i, c := range id {
		valid = valid && (c == '_' || 'a' <= c && c <= 'z' || i > 0 && ('0' <= c && c <= '9' || c == '.' || c == '-' || c == '/'))
	}
	return valid
}
