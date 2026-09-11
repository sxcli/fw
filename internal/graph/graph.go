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

package graph

import (
	"reflect"

	"sxcli.dev/conf/fail"
	"sxcli.dev/fw/internal/registry"
	"sxcli.dev/rules/solver"
)

// Resolve computes the composition of one invocation: seed the
// resolved service set with the given root descriptor and every
// forced Enable, expand it through the inject fields, resolve every
// member's bindings against the final resolved service set, and
// order it dependencies-first. The root is any descriptor — a
// registered service (resolving its own service set) or a
// virtual one the caller composed (the framework's core node); it is
// never disable-checked, that is the caller's courtesy. Violations are
// recorded into c; when c grew, the Result must not be used.
func Resolve(c *fail.Collector, reg *registry.Registry, root *registry.Descriptor, ctl Controls) Result {
	// the graph package is the solver's reflect-side adapter: it
	// renders descriptors into declared-fact members, lets
	// sxcli.dev/rules/solver decide — the SAME decisions sxcli-vet
	// judges with — and maps the verdict back onto descriptors.
	members := make([]solver.Member, 0, len(reg.All()))
	for _, d := range reg.All() {
		members = append(members, renderMember(d))
	}
	verdict := solver.Solve(members, renderMember(root), solver.Controls(ctl))
	for _, v := range verdict.Violations {
		body := v.Body
		if v.Ambiguous {
			// the nudge is OUR vocabulary — the solver's body stays
			// tool-neutral, and vet (being the tool) never says this
			body += " (the sxcli-vet tool catches this before it runs)"
		}
		if v.Owner == "" {
			c.Fail("%s", body)
		} else {
			c.Fail("service %q field %s: %s", v.Owner, v.Dep, body)
		}
	}
	var out Result
	out.UnusedOverrides = verdict.UnusedOverrides
	out.Cycles = verdict.Cycles
	if len(verdict.Violations) == 0 {
		byID := map[string]*registry.Descriptor{root.ID: root}
		depsByID := map[string][]registry.DepField{root.ID: root.Deps}
		for _, d := range reg.All() {
			byID[d.ID] = d
			depsByID[d.ID] = d.Deps
		}
		for _, bm := range verdict.Ordered {
			m := Member{Desc: byID[bm.ID]}
			for bi, b := range bm.Bindings {
				binding := Binding{Dep: depsByID[bm.ID][bi]}
				for _, target := range b.Targets {
					binding.Targets = append(binding.Targets, byID[target])
				}
				m.Bindings = append(m.Bindings, binding)
			}
			out.Ordered = append(out.Ordered, m)
		}
	}
	return out
}

// renderMember renders one descriptor into the solver's declared-fact
// vocabulary: type identities become opaque strings, exactly the
// rendering sxcli-vet's go/types side produces.
func renderMember(d *registry.Descriptor) solver.Member {
	m := solver.Member{
		ID:       d.ID,
		Core:     d.Core,
		Concrete: typeID(d.Concrete),
		Alias:    d.Alias,
		Ranked:   d.Ranked,
	}
	for _, it := range d.Provides {
		m.Provides = append(m.Provides, typeID(it))
	}
	for _, dep := range d.Deps {
		m.Deps = append(m.Deps, solver.Dep{
			Name:     dep.Name,
			TypeID:   typeID(dep.Type),
			IsIface:  dep.Type.Kind() == reflect.Interface,
			IDs:      dep.IDs,
			Optional: dep.Optional,
			IsSlice:  dep.IsSlice,
		})
	}
	return m
}

// typeID renders unambiguous type identity: String() can collide
// across packages, pkgpath-qualified names cannot — one rendering
// rule for every rules-module consumer.
func typeID(t reflect.Type) string {
	if t == nil {
		return ""
	}
	if t.PkgPath() != "" {
		return t.PkgPath() + "." + t.Name()
	}
	return t.String()
}

// Subtree returns the sub-result reachable from the member named id
// through its resolved bindings — the member itself included, the main
// dependency order inherited, no re-resolution: the bindings ARE the
// edges. ok is false when id is not in the resolved service set
// (disabled, or never resolved in).
func (res Result) Subtree(id string) (Result, bool) {
	byID := map[string]Member{}
	for _, m := range res.Ordered {
		byID[m.Desc.ID] = m
	}
	var out Result
	_, ok := byID[id]
	if ok {
		keep := map[string]bool{}
		queue := []string{id}
		for len(queue) > 0 {
			next := queue[0]
			queue = queue[1:]
			if !keep[next] {
				keep[next] = true
				for _, b := range byID[next].Bindings {
					for _, target := range b.Targets {
						queue = append(queue, target.ID)
					}
				}
			}
		}
		for _, m := range res.Ordered {
			if keep[m.Desc.ID] {
				out.Ordered = append(out.Ordered, m)
			}
		}
	}
	return out, ok
}
