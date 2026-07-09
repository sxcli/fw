package graph

import (
	"reflect"

	"github.com/sxcli/sxcli-fw/internal/fail"
	"github.com/sxcli/sxcli-fw/internal/registry"
)

// Resolve computes the composition of one invocation: seed the closure
// with the dispatched applet, the always-on services and every forced
// Enable, expand it through the inject fields, resolve every member's
// bindings against the final closure, and order it dependencies-first.
// Violations are recorded into c; when c grew, the Result must not be
// used.
func Resolve(c *fail.Collector, reg *registry.Registry, appletID string, alwaysOn []string, ctl Controls) Result {
	r := &resolver{
		reg:      reg,
		c:        c,
		disabled: map[string]bool{},
		override: map[string]string{},
		closure:  map[string]bool{},
	}
	before := c.Len()
	r.validateControls(ctl)
	if c.Len() == before {
		r.expand(r.seeds(appletID, alwaysOn, ctl.Enable))
	}
	if c.Len() == before {
		r.order(r.bind())
	}
	return r.result
}

// fail records a resolution violation.
func (r *resolver) fail(format string, args ...any) {
	r.c.Fail(format, args...)
}

func (r *resolver) validateControls(ctl Controls) {
	for _, id := range ctl.Disable {
		if _, ok := r.reg.ByID(id); ok {
			r.disabled[id] = true
		} else {
			r.fail("disable: unknown service id %q", id)
		}
	}
	for _, id := range ctl.Enable {
		if _, ok := r.reg.ByID(id); !ok {
			r.fail("enable: unknown service id %q", id)
		} else if r.disabled[id] {
			r.fail("service %q is both enabled and disabled", id)
		}
	}
	for from, to := range ctl.Override {
		if _, ok := r.reg.ByID(to); ok {
			r.override[from] = to
		} else {
			r.fail("override: unknown substitute id %q for %q", to, from)
		}
	}
}

// seeds returns the closure roots: the applet, every always-on service
// and every forced Enable. Disabled always-on services are silently
// skipped — disabling one is legitimate user intent.
func (r *resolver) seeds(appletID string, alwaysOn []string, enable []string) []*registry.Descriptor {
	var out []*registry.Descriptor
	if d, ok := r.reg.ByID(appletID); ok {
		if r.disabled[appletID] {
			r.fail("applet %q is disabled", appletID)
		} else {
			out = append(out, d)
		}
	} else {
		r.fail("applet %q is not registered", appletID)
	}
	for _, id := range alwaysOn {
		if d, ok := r.reg.ByID(id); ok {
			if !r.disabled[id] {
				out = append(out, d)
			}
		} else {
			r.fail("always-on service %q is not registered", id)
		}
	}
	for _, id := range enable {
		if d, ok := r.reg.ByID(id); ok && !r.disabled[id] {
			out = append(out, d)
		}
	}
	return out
}

// expand grows the closure to a fixpoint over the inject fields.
func (r *resolver) expand(queue []*registry.Descriptor) {
	for len(queue) > 0 {
		d := queue[0]
		queue = queue[1:]
		if !r.closure[d.ID] {
			r.closure[d.ID] = true
			for _, dep := range d.Deps {
				queue = append(queue, r.expandDep(d, dep)...)
			}
		}
	}
}

// expandDep resolves one dependency field to the descriptors it pulls
// into the closure.
func (r *resolver) expandDep(owner *registry.Descriptor, dep registry.DepField) []*registry.Descriptor {
	var out []*registry.Descriptor
	if len(dep.IDs) > 0 {
		for _, raw := range dep.IDs {
			id := r.mapped(raw)
			if target, ok := r.reg.ByID(id); ok {
				if r.disabled[id] {
					if !dep.Optional {
						r.fail("service %q field %s: required dependency %q is disabled", owner.ID, dep.Name, id)
					}
				} else if r.matches(target, dep) {
					out = append(out, target)
				} else {
					r.fail("service %q field %s: service %q does not satisfy %s", owner.ID, dep.Name, id, dep.Type)
				}
			} else {
				r.fail("service %q field %s: unknown service id %q", owner.ID, dep.Name, id)
			}
		}
	} else {
		out = r.candidates(dep)
		if !dep.IsSlice && len(out) > 1 {
			out = out[:1]
		}
		if len(out) == 0 && !dep.Optional {
			r.fail("service %q field %s: no registered service satisfies %s", owner.ID, dep.Name, dep.Type)
		}
	}
	return out
}

// mapped applies the override remapping to one requested id.
func (r *resolver) mapped(id string) string {
	out := id
	if to, ok := r.override[id]; ok {
		out = to
	}
	return out
}

// candidates returns every non-disabled registered service matching the
// dependency's type, in registration order.
func (r *resolver) candidates(dep registry.DepField) []*registry.Descriptor {
	var out []*registry.Descriptor
	for _, d := range r.reg.All() {
		if !r.disabled[d.ID] && r.matches(d, dep) {
			out = append(out, d)
		}
	}
	return out
}

// matches reports whether target satisfies the dependency's type:
// a declared interface for interface fields, the exact concrete type for
// pointer fields.
func (r *resolver) matches(target *registry.Descriptor, dep registry.DepField) bool {
	var ok bool
	if dep.Type.Kind() == reflect.Interface {
		for _, it := range target.Provides {
			ok = ok || it == dep.Type
		}
	} else {
		ok = target.Concrete == dep.Type
	}
	return ok
}

// bind resolves every closure member's fields against the final closure,
// in registration order. It runs after expansion because slice fields
// gather every closure member of their type, including services that
// joined through other paths after the owner was expanded.
func (r *resolver) bind() []Member {
	var members []Member
	for _, d := range r.reg.All() {
		if r.closure[d.ID] {
			m := Member{Desc: d}
			for _, dep := range d.Deps {
				m.Bindings = append(m.Bindings, Binding{Dep: dep, Targets: r.bindDep(dep)})
			}
			members = append(members, m)
		}
	}
	return members
}

// bindDep computes the final injection targets of one dependency field.
func (r *resolver) bindDep(dep registry.DepField) []*registry.Descriptor {
	var out []*registry.Descriptor
	if dep.IsSlice {
		for _, d := range r.reg.All() {
			if r.closure[d.ID] && r.matches(d, dep) {
				out = append(out, d)
			}
		}
	} else if len(dep.IDs) > 0 {
		id := r.mapped(dep.IDs[0])
		if target, ok := r.reg.ByID(id); ok && r.closure[id] {
			out = append(out, target)
		}
	} else {
		if cands := r.candidates(dep); len(cands) > 0 {
			out = append(out, cands[0])
		}
	}
	return out
}

// order emits members dependencies-first: strongly connected components
// via Tarjan, the condensation in topological order with ties broken by
// registration order, registration order within a component. Components
// larger than one member — and self-loops — are reported as cycles.
func (r *resolver) order(members []Member) {
	n := len(members)
	pos := make(map[string]int, n)
	for i := range members {
		pos[members[i].Desc.ID] = i
	}
	needs := make([][]int, n)
	selfLoop := make([]bool, n)
	for i := range members {
		for _, b := range members[i].Bindings {
			for _, target := range b.Targets {
				if j := pos[target.ID]; j == i {
					selfLoop[i] = true
				} else {
					needs[i] = append(needs[i], j)
				}
			}
		}
	}
	comp, ncomp := tarjan(needs)
	groups := make([][]int, ncomp)
	for i := 0; i < n; i++ {
		groups[comp[i]] = append(groups[comp[i]], i) // ascending → registration order
	}
	cneeds := make([][]int, ncomp)
	seen := make([]map[int]bool, ncomp)
	for c := 0; c < ncomp; c++ {
		seen[c] = map[int]bool{}
	}
	for i := 0; i < n; i++ {
		for _, j := range needs[i] {
			if comp[j] != comp[i] && !seen[comp[i]][comp[j]] {
				seen[comp[i]][comp[j]] = true
				cneeds[comp[i]] = append(cneeds[comp[i]], comp[j])
			}
		}
	}
	for i := 0; i < n; i++ {
		if c := comp[i]; groups[c][0] == i && (len(groups[c]) > 1 || selfLoop[i]) {
			var ids []string
			for _, j := range groups[c] {
				ids = append(ids, members[j].Desc.ID)
			}
			r.result.Cycles = append(r.result.Cycles, ids)
		}
	}
	done := make([]bool, ncomp)
	for emitted := 0; emitted < ncomp; emitted++ {
		best := -1
		for c := 0; c < ncomp; c++ {
			ready := !done[c]
			for _, need := range cneeds[c] {
				ready = ready && done[need]
			}
			if ready && (best < 0 || groups[c][0] < groups[best][0]) {
				best = c
			}
		}
		if best < 0 {
			r.fail("internal: condensation is not a DAG")
			emitted = ncomp
		} else {
			done[best] = true
			for _, i := range groups[best] {
				r.result.Ordered = append(r.result.Ordered, members[i])
			}
		}
	}
}

// tarjan computes strongly connected components of adj; it returns the
// component id of every node and the component count.
func tarjan(adj [][]int) ([]int, int) {
	n := len(adj)
	comp := make([]int, n)
	index := make([]int, n)
	low := make([]int, n)
	onStack := make([]bool, n)
	for i := 0; i < n; i++ {
		index[i] = -1
	}
	var stack []int
	next, ncomp := 0, 0
	var strong func(v int)
	strong = func(v int) {
		index[v] = next
		low[v] = next
		next++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range adj[v] {
			if index[w] < 0 {
				strong(w)
				if low[w] < low[v] {
					low[v] = low[w]
				}
			} else if onStack[w] && index[w] < low[v] {
				low[v] = index[w]
			}
		}
		if low[v] == index[v] {
			for done := false; !done; {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				comp[w] = ncomp
				done = w == v
			}
			ncomp++
		}
	}
	for v := 0; v < n; v++ {
		if index[v] < 0 {
			strong(v)
		}
	}
	return comp, ncomp
}
