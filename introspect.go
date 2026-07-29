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

	"sxcli.dev/conf/engine"
	"sxcli.dev/conf/fail"
	"sxcli.dev/fw/internal/graph"
	"sxcli.dev/fw/internal/registry"
)

// ArgInfo is the system vocabulary's argument description —
// re-exported so fw-side consumers keep one import.
type ArgInfo = system.ArgInfo

// Introspector is the TARGET-SCOPED read-only view behind
// system.Introspector: one applet's resolved graph, built from the
// attach-time catalog snapshot and NOTHING else — no config files, no
// location search, no environment. Same binary, same target, same
// answer, always; ejection cannot shrink the snapshot, so completion
// keeps its facts while its own closure stays as lean as any other.
// A nil target is the binary view: applet listing, no closure.
type Introspector struct {
	cat     *catalog             // attach-time snapshot; data-plane only
	target  *registry.Descriptor // nil: the binary view
	ordered []graph.Member       // the target's resolved closure, composed order
}

// Applets returns the primary alias of every registered public
// applet, in composed order — the operator vocabulary: these are the
// selectors a completion offers as first words. Hidden and System
// applets are omitted: they are not commands offered to a human, and
// a completion must not offer what a human should not type.
// Binary-level: the same on every view.
func (i *Introspector) Applets() []string {
	var out []string
	for _, d := range i.cat.reg.All() {
		if d.Concrete.Implements(appletType) && !d.Hidden {
			out = append(out, primaryAlias(d))
		}
	}
	return out
}

// SingleApplet reports the applet that would run with no selector
// word: in single-applet mode — exactly one non-System applet
// registered — its primary alias and true, otherwise "" and false.
// This is dispatch-mode truth straight from the dispatch rules, and
// consumers must not re-derive it from Applets: that listing is
// public-only, while a Hidden non-System applet still counts for the
// mode. Binary-level: the same on every view.
func (i *Introspector) SingleApplet() (string, bool) {
	alias := ""
	n := 0
	for _, d := range i.cat.reg.All() {
		if d.Concrete.Implements(appletType) && !d.System {
			n++
			alias = primaryAlias(d)
		}
	}
	ok := n == 1
	if !ok {
		alias = ""
	}
	return alias, ok
}

// ConfigExtensions returns every config file extension this binary can
// read: "json" first, then each registered format provider's
// extensions in registration order, deduplicated. Binary-level.
func (i *Introspector) ConfigExtensions() []string {
	out := []string{"json"}
	for _, d := range i.cat.reg.All() {
		if providesType(d, providerType) {
			if p, ok := d.Instance.(ConfigFormatProvider); ok {
				for _, ext := range p.Extensions() {
					if !contains(out, ext) {
						out = append(out, ext)
					}
				}
			}
		}
	}
	return out
}

// Services returns the primary aliases of the TARGET's resolved
// graph — the core leading (a virtual root is truthfully part of
// every closure, spec §5), then the closure members in COMPOSED
// order, matching every other listing. The binary view has no
// closure: nil.
func (i *Introspector) Services() []string {
	if i.target == nil {
		return nil
	}
	out := []string{CoreAlias}
	for _, m := range i.ordered {
		out = append(out, primaryAlias(m.Desc))
	}
	return out
}

// Describe returns the long-form description of a member of the
// target's resolved graph — alias or id, both vocabularies are legal
// inside the graph — or "" for anything outside it: introspection
// does not reach past the closure.
func (i *Introspector) Describe(ref string) string {
	out := ""
	if i.target != nil {
		if ref == CoreAlias || ref == CoreID {
			out = "the framework core: configuration, dispatch, resolution and lifecycle; the virtual root every closure grows from"
		} else if d, member := i.member(ref); member {
			if meta, has := d.Metadata.(*engine.Meta); has {
				out = meta.Description
			}
		}
	}
	return out
}

// member resolves a reference — alias or id — to a descriptor of the
// target's closure; anything else, registered or not, is not a member.
func (i *Introspector) member(ref string) (*registry.Descriptor, bool) {
	d, found := i.cat.byAlias[ref]
	if !found {
		d, found = i.cat.reg.ByID(ref)
	}
	if found {
		for _, m := range i.ordered {
			if m.Desc == d {
				return d, true
			}
		}
	}
	return nil, false
}

// Arguments returns the target's closure-true argument schema, built
// from the catalog snapshot alone — registration-level truth, no
// files, no environment, no controls. args are the words BEFORE the
// completion cursor; today they are inert (reserved for the
// explicit-control-vocabulary era, when line-carried controls
// participate in the solve). The binary view answers nil.
func (i *Introspector) Arguments(_ []string) []ArgInfo {
	if i.target == nil {
		return nil
	}
	c := &fail.Collector{}
	var core engine.Core
	var ctrl coreControls
	var kn upgradeKnobs
	sch := engine.NewSchema(c, primaryAlias(i.target), coreContribs(&core, &ctrl, &kn), sections(i.ordered), i.cat.suppressed)
	if c.Len() != 0 {
		// the closure solved at view construction; a schema violation
		// here is a startup-checked inconsistency — offer nothing
		// rather than half of something
		return nil
	}
	return argInfos(sch)
}

// argInfos maps a schema to its public description.
func argInfos(sch *engine.Schema) []ArgInfo {
	var out []ArgInfo
	for _, section := range sch.HelpSections() {
		for _, f := range section.Fields {
			out = append(out, ArgInfo{
				Service: f.Owner,
				Long:    f.Long,
				Short:   f.Short,
				Env:     f.EnvName,
				Usage:   f.Usage,
				Type:    f.Type,
				IsSlice: f.IsSlice,
				Allowed: f.Allowed,
				Doc:     f.Doc,
				Hint:    ValueHint(f.Hint),
			})
		}
	}
	return out
}

// the concrete Introspector IS the system vocabulary's interface.
var _ system.Introspector = (*Introspector)(nil)

// introspector builds the view for one dispatch name against this
// catalog: "" is the binary view; an unknown name, a non-applet, or
// a target that cannot resolve is nil. The one construction path —
// the system service delegates here, and so do tests.
func (ca *catalog) introspector(applet string) *Introspector {
	if applet == "" {
		return &Introspector{cat: ca}
	}
	d, known := ca.byAlias[applet] // dispatch names only — ids do not resolve here
	if !known || !d.Concrete.Implements(appletType) {
		return nil
	}
	c := &fail.Collector{}
	root := ca.coreRoot(c, d, nil)
	var res graph.Result
	if c.Len() == 0 {
		res = graph.Resolve(c, ca.reg, root, graph.Controls{})
	}
	if c.Len() != 0 {
		// the target cannot resolve against its own catalog — a
		// startup-checked inconsistency; offer nothing
		return nil
	}
	// members in COMPOSED order, exactly as plan() builds the real
	// schema: the spec promises Order drives listings, and NewSchema's
	// first-come-first-served short forms make section order SEMANTIC
	// — a view in resolution order would hand shorts to the wrong
	// owner. The virtual root is never stored in the catalog, so it
	// cannot appear here; the view synthesizes the core itself.
	keep := map[string]bool{}
	for _, m := range res.Ordered {
		keep[m.Desc.ID] = true
	}
	var members []graph.Member
	for _, cd := range ca.reg.All() {
		if keep[cd.ID] {
			members = append(members, graph.Member{Desc: cd})
		}
	}
	return &Introspector{cat: ca, target: d, ordered: members}
}
