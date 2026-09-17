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

// Package controls adds the operator's service controls to a binary:
// --disable, --enable and --override. The import is the opt-in —
//
//	import _ "sxcli.dev/fw/controls"
//
// a binary without it carries none of this code and the arguments
// are unknown. Reshaping the resolved service set at invocation time
// is a deliberate capability; most binaries never want it.
package controls

import (
	"strings"

	"sxcli.dev/conf/engine"
	"sxcli.dev/conf/fail"
	"sxcli.dev/fw/internal/ctlhook"
	"sxcli.dev/fw/internal/graph"
	"sxcli.dev/fw/system"
	"sxcli.dev/rules/solver"
)

// knobs is the controls' contribution to the composite core section,
// riding the same operator surfaces as the engine's own knobs.
type knobs struct {
	Disable  []string `json:"disable" conf:"disable" env:"-" usage:"service ids to remove from the resolved service set"`
	Enable   []string `json:"enable" conf:"enable" env:"-" usage:"service ids to force into the resolved service set"`
	Override []string `json:"override" conf:"override" env:"-" usage:"dependency remapping in from=to form"`
}

// knobsMeta carries the controls' hints for introspection and
// completion. Override takes from=to pairs, not plain service ids —
// no honest hint fits; tooling that understands the pair form can
// still act on the field by name.
var knobsMeta = &engine.Meta{Fields: map[string]engine.FieldMeta{
	"Disable": {Hint: system.HintServiceID},
	"Enable":  {Hint: system.HintServiceID},
}}

// coreRef reports whether the reference names the framework core
// itself — the virtual root is never a registry member, so the
// solver's core-family verdict cannot reach it; this guard says the
// same words (solver.CoreControlRule) for the same act.
func coreRef(ref string) bool {
	return ref == engine.CoreID || ref == ctlhook.CoreID
}

// resolveRef resolves an operator-supplied service reference — an
// alias or an id, both legal in every control slot. The vocabularies
// are disjoint by grammar (an alias never contains '/', an id always
// does), so the token's shape says which dictionary to open — no tie
// is expressible. Applets are not services from the operator's seat,
// the dispatched one included; dormant ones are not even here.
func resolveRef(view ctlhook.View, ref string) (ctlhook.Ref, bool) {
	var r ctlhook.Ref
	ok := false
	if strings.Contains(ref, "/") {
		r, ok = view.ByID(ref)
	} else {
		r, ok = view.ByAlias(ref)
	}
	if ok && r.Applet {
		ok = false
	}
	return r, ok
}

// translate turns the operator's service references into graph
// identities: disable/enable/override values accept BOTH vocabularies
// — aliases (what operators speak) and ids (what inject tags and docs
// say). The graph stays identity-based and ignorant of aliases.
// Override's from side is special: it matches dependency REFERENCES
// (tag strings), which may name nothing registered — an unresolvable
// from stays raw and at worst earns the unused-override warning.
func translate(c *fail.Collector, view ctlhook.View, raw any) graph.Controls {
	k := raw.(*knobs)
	ctl := graph.Controls{}
	for _, ref := range k.Disable {
		if coreRef(ref) {
			c.Fail(solver.CoreControlRule, "disable", ref)
		} else if r, ok := resolveRef(view, ref); ok {
			ctl.Disable = append(ctl.Disable, r.ID)
		} else {
			c.Fail("disable: unknown service %q", ref)
		}
	}
	for _, ref := range k.Enable {
		if coreRef(ref) {
			c.Fail(solver.CoreControlRule, "enable", ref)
		} else if r, ok := resolveRef(view, ref); ok {
			ctl.Enable = append(ctl.Enable, r.ID)
		} else {
			c.Fail("enable: unknown service %q", ref)
		}
	}
	for _, entry := range k.Override {
		from, to, wellFormed := strings.Cut(entry, "=")
		if wellFormed && from != "" && to != "" {
			if coreRef(from) || coreRef(to) {
				offender := from
				if coreRef(to) {
					offender = to
				}
				c.Fail(solver.CoreControlRule, "override", offender)
				continue
			}
			if ctl.Override == nil {
				ctl.Override = map[string]string{}
			}
			if fromR, ok := resolveRef(view, from); ok {
				if fromR.Core {
					// the solver's core-family verdict is unreachable
					// when the entry never forms (an unknown to side
					// would fail first); same act, same words
					c.Fail(solver.CoreControlRule, "override", from)
					continue
				}
				from = fromR.ID
			}
			if toR, ok := resolveRef(view, to); ok {
				ctl.Override[from] = toR.ID
			} else {
				c.Fail("override: unknown substitute %q for %q", to, from)
			}
		} else {
			c.Fail("override %q: expected from=to", entry)
		}
	}
	return ctl
}

func init() {
	ctlhook.Registered = &ctlhook.Impl{
		New:       func() any { return &knobs{} },
		Meta:      knobsMeta,
		Translate: translate,
		FeatureLongs: map[int]string{
			FeatureDisableService:  "disable",
			FeatureEnableService:   "enable",
			FeatureOverrideService: "override",
		},
	}
}
