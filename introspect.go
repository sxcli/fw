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

package sxclifw

import (
	"errors"
	"fmt"
	"reflect"

	"sxcli.dev/fw/internal/config"
	"sxcli.dev/fw/internal/fail"
	"sxcli.dev/fw/internal/graph"
	"sxcli.dev/fw/internal/registry"
)

// introspectionID is the reserved service id of the core's Introspector.
const introspectionID = "introspection"

// ArgInfo describes one config struct field of an applet's closure —
// the schema unit completions and documentation generators consume. A
// field with an empty Long is not settable from the command line; one
// with an empty Env is not settable from the environment; both empty
// means file-only. For slices, Type is the element type.
type ArgInfo struct {
	Service string       // owning service id, "core" included
	Long    string       // long argument name, without dashes
	Short   string       // single-character short form
	Env     string       // environment variable name
	Usage   string       // untranslated help text; render via Tr
	Type    reflect.Type // field type; element type for slices
	IsSlice bool         // repeatable argument, comma-separated env, json array
	Allowed []any        // closed value domain from registration Metadata; values are of Type
	Doc     string       // long-form description from registration Metadata
	Hint    ValueHint    // advisory value denotation from registration Metadata; never enforced
}

// Introspector is the core's read-only view of the binary's
// composition, for services that implement completions, documentation
// generators and similar meta features outside the core. There is
// exactly one: the core constructs and registers it itself under the
// reserved id "introspection" — it reports the composition truth, and
// truth does not federate. Consumers inject it by concrete type:
//
//	type CompletionApplet struct {
//		I *sxclifw.Introspector `inject:""`
//	}
//
// The one price of introspection: a closure containing the Introspector
// is never ejected — enumerating the binary requires keeping the
// registry alive. Only invocations that injected it pay that.
type Introspector struct {
	rt *runtime
}

// Applets returns the ids of every registered public applet, in
// registration order. Hidden and System applets are omitted: they are
// not commands offered to a human, and a completion must not offer
// what a human should not type.
func (i *Introspector) Applets() []string {
	var out []string
	for _, d := range i.rt.reg.All() {
		if _, isApplet := d.Instance.(Applet); isApplet && !d.Hidden {
			out = append(out, d.ID)
		}
	}
	return out
}

// SingleApplet reports the applet that would run with no selector
// word: in single-applet mode — exactly one non-System applet
// registered — its id and true, otherwise "" and false. This is
// dispatch-mode truth straight from the dispatch rules, and consumers
// must not re-derive it from Applets: that listing is public-only,
// while a Hidden non-System applet still counts for the mode.
func (i *Introspector) SingleApplet() (string, bool) {
	id := ""
	n := 0
	for _, d := range i.rt.reg.All() {
		if _, isApplet := d.Instance.(Applet); isApplet {
			if !d.System {
				n++
				id = d.ID
			}
		}
	}
	ok := n == 1
	if !ok {
		id = ""
	}
	return id, ok
}

// Services returns the ids of every registered service — applets
// included — in registration order.
func (i *Introspector) Services() []string {
	// the core is a virtual root, not a registry entry — its presence
	// here is synthesized, because it is truthfully part of every
	// binary (spec §5)
	out := []string{CoreAlias}
	for _, d := range i.rt.reg.All() {
		out = append(out, d.ID)
	}
	return out
}

// Arguments returns the argument schema the given applet would have if
// invoked with args: the real planning pipeline runs — lenient core
// peek honoring an in-line --config, file loading, controls from every
// source, closure resolution — with zero side effects (nothing is
// written, ejected or mutated; --write-config and --help in args are
// inert data). Suppressed features are absent, exactly as at execution.
//
// args must be the words BEFORE the completion cursor, not including
// the word being completed: a half-typed token passed as data would be
// planned as configuration.
//
// The result is best-effort: when planning collects violations (a
// broken config file, an unknown id in a control), Arguments retries
// with no files and no controls — the registration-level schema — and
// returns that alongside the joined violations. A non-nil error
// therefore does not mean an empty result; callers wanting candidates
// may ignore it, callers wanting diagnostics must not.
func (i *Introspector) Arguments(appletID string, args []string) ([]ArgInfo, error) {
	var out []ArgInfo
	var err error
	if d, registered := i.rt.reg.ByID(appletID); !registered {
		err = fmt.Errorf("introspection: %q is not registered", appletID)
	} else if _, isApplet := d.Instance.(Applet); !isApplet {
		err = fmt.Errorf("introspection: %q is not an applet", appletID)
	} else {
		c := &fail.Collector{}
		p := i.rt.plan(c, d, args)
		if c.Len() == 0 {
			out = argInfos(p.sch)
		} else {
			err = errors.Join(c.All()...)
			fallback := &fail.Collector{}
			var core config.Core
			root := i.rt.coreRoot(fallback, d, nil)
			var res graph.Result
			if fallback.Len() == 0 {
				res = graph.Resolve(fallback, i.rt.reg, root, graph.Controls{})
			}
			if fallback.Len() == 0 {
				var members []*registry.Descriptor
				for _, m := range res.Ordered {
					members = append(members, m.Desc)
				}
				sch := config.NewSchema(fallback, appletID, &core, members, i.rt.suppressed)
				if fallback.Len() == 0 {
					out = argInfos(sch)
				}
			}
		}
	}
	return out, err
}

// Describe returns the long-form description a service declared via
// WithMetadata, or "" when it declared none (or the id is unknown).
func (i *Introspector) Describe(serviceID string) string {
	out := ""
	if serviceID == CoreAlias {
		out = "the framework core: configuration, dispatch, resolution and lifecycle; the virtual root every closure grows from"
	} else if d, registered := i.rt.reg.ByID(serviceID); registered {
		if meta, has := d.Metadata.(*config.Meta); has {
			out = meta.Description
		}
	}
	return out
}

// argInfos maps a schema to its public description.
func argInfos(sch *config.Schema) []ArgInfo {
	var out []ArgInfo
	for _, section := range sch.HelpSections() {
		for _, f := range section.Fields {
			out = append(out, ArgInfo{
				Service: f.ServiceID,
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

// ConfigExtensions returns every config file extension this binary can
// read: "json" first, then each registered format provider's
// extensions in registration order, deduplicated.
func (i *Introspector) ConfigExtensions() []string {
	out := []string{"json"}
	for _, d := range i.rt.reg.All() {
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
