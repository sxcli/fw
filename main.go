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
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"sxcli.dev/fw/conf"
	"sxcli.dev/fw/internal/fail"
	"sxcli.dev/fw/internal/graph"
	"sxcli.dev/fw/internal/logging"
	"sxcli.dev/fw/internal/registry"
)

// Main is the busybox-compatibility sugar of the composition model:
// accept everything cataloged and run. Exactly
// Builder().AcceptAll().Main() — magic you opt into by name. It never
// returns. Binaries wanting composition control use the Builder
// directly.
func Main() {
	Builder().AcceptAll().Main()
}

// positionals holds the trailing bare arguments of the invocation.
var positionals []string

// Positionals returns the trailing bare arguments of the invocation.
// The current version collects them without routing; a routing
// mechanism is a future design point.
func Positionals() []string {
	return positionals
}

var handlerType = reflect.TypeOf((*slog.Handler)(nil)).Elem()
var providerType = reflect.TypeOf((*ConfigFormatProvider)(nil)).Elem()
var translatorType = reflect.TypeOf((*Translator)(nil)).Elem()

// run is the whole pipeline; Main wraps it with the process exit.
// Framework-level failures exit 2; otherwise the exit code is the
// applet's.
func run(rt *runtime) int {
	code := 2
	previous := slog.Default()
	defer slog.SetDefault(previous)
	buffer := logging.NewBuffer()
	slog.SetDefault(slog.New(buffer))
	// the core's own service: the read-only composition view, cold
	// unless something injects it — a full citizen of the identity
	// model: path identity, operator alias. Squatting the concrete
	// type is refused explicitly (the catalog tolerates same-type
	// entries, so the old duplicate-type collision no longer defends
	// this): there is exactly one Introspector, and it is the core's.
	for _, d := range rt.reg.All() {
		if d.Concrete == reflect.TypeOf(&Introspector{}) {
			rt.c.Fail("service %q: the Introspector's concrete type is reserved for the core", d.ID)
		}
	}
	rt.reg.Commit(&registry.Descriptor{
		ID:       IntrospectionID,
		Aliases:  []string{IntrospectionAlias},
		Instance: &Introspector{rt: rt},
		Concrete: reflect.TypeOf(&Introspector{}),
	})
	// the core's translator dependency: exactly one service may
	// provide it (spec §7); two catalog systems in one binary is a
	// developer error, reported like every other violation
	for _, d := range rt.reg.All() {
		if providesType(d, translatorType) {
			if rt.translatorID == "" {
				rt.translatorID = d.ID
			} else {
				rt.c.Fail("services %q and %q both provide Translator; a binary has exactly one", rt.translatorID, d.ID)
			}
		}
	}
	// the operator-name index: every alias resolves to its service.
	// Composed-alias collisions were Build violations — a clash here
	// is a framework bug, reported not swallowed.
	rt.byAlias = map[string]*registry.Descriptor{}
	for _, d := range rt.reg.All() {
		for _, a := range d.Aliases {
			if prev, taken := rt.byAlias[a]; taken && prev != d {
				rt.c.Fail("operator name %q resolves to both %q and %q", a, prev.ID, d.ID)
			} else {
				rt.byAlias[a] = d
			}
		}
	}
	if rt.c.Len() > 0 {
		rt.report(buffer)
	} else if d, applet, args, ok := rt.dispatch(); ok {
		code = rt.execute(buffer, d, applet, args)
	}
	return code
}

// resolveRef resolves an operator-supplied service reference — an
// alias or an id; both vocabularies are legal (aliases are what
// operators speak, ids are what inject tags and documentation say,
// and --override's from side inherently speaks id). Convention keeps
// the two disjoint (ids are path-shaped); the pathological tie — the
// string naming one service's alias and a DIFFERENT service's id — is
// a loud violation, never a silent pick.
func (rt *runtime) resolveRef(c *fail.Collector, ref string) (*registry.Descriptor, bool) {
	byAlias, aliasHit := rt.byAlias[ref]
	byID, idHit := rt.reg.ByID(ref)
	var out *registry.Descriptor
	ok := false
	if aliasHit && idHit && byAlias != byID {
		c.Fail("%q names the alias of %q and the id of %q — say which", ref, byAlias.ID, byID.ID)
	} else if aliasHit {
		out, ok = byAlias, true
	} else if idHit {
		out, ok = byID, true
	}
	return out, ok
}

// dispatch picks the applet per the spec rules: single-applet mode
// (only non-System applets count, with the System-selector carve-out),
// else first-bare-argument selector (Hidden and System applets are
// selectable like any other), else basename(argv[0]) among non-Hidden
// applets. Selectors and basenames are ALIASES — any declared name
// selects; listings show the primary.
func (rt *runtime) dispatch() (*registry.Descriptor, Applet, []string, bool) {
	var applets []*registry.Descriptor // every applet, any visibility
	var public []*registry.Descriptor  // non-Hidden: what usage may list
	var sole *registry.Descriptor      // the single non-System applet, when nMain == 1
	nMain := 0
	for _, d := range rt.reg.All() {
		if _, isApplet := d.Instance.(Applet); isApplet {
			applets = append(applets, d)
			if !d.Hidden {
				public = append(public, d)
			}
			if !d.System {
				nMain++
				sole = d
			}
		}
	}
	var picked *registry.Descriptor
	var applet Applet
	var args []string
	var rest []string
	if len(rt.argv) > 1 {
		rest = rt.argv[1:]
	}
	ok := false
	if len(applets) == 0 {
		rt.usage(public, Tr("no applets are registered in this binary"))
	} else if nMain == 1 {
		// Single-applet mode: the sole non-System applet always runs
		// and the whole vector is its data — except a first bare token
		// naming a System applet, the tooling entry path.
		if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
			if d, found := rt.byAlias[rest[0]]; found && d.System {
				if a, isApplet := d.Instance.(Applet); isApplet {
					picked, applet, args, ok = d, a, rest[1:], true
				}
			}
		}
		if !ok {
			picked, applet, args, ok = sole, sole.Instance.(Applet), rest, true
		}
	} else if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		if d, found := rt.byAlias[rest[0]]; found {
			if a, isApplet := d.Instance.(Applet); isApplet {
				picked, applet, args, ok = d, a, rest[1:], true
			}
		}
		if !ok {
			rt.usage(public, Tr("{name} does not name an applet", "name", rest[0]))
		}
	} else {
		name := ""
		if len(rt.argv) > 0 {
			name = binaryBasename(rt.argv[0])
		}
		if d, found := rt.byAlias[name]; found && !d.Hidden {
			if a, isApplet := d.Instance.(Applet); isApplet {
				picked, applet, args, ok = d, a, rest, true
			}
		}
		if !ok {
			rt.usage(public, Tr("{name} does not name an applet", "name", name))
		}
	}
	return picked, applet, args, ok
}

// usage prints the dispatch-failure usage to stderr; Hidden and System
// applets are omitted from the list (single-applet mode never fails
// dispatch, so the list rendering only ever runs in selector modes).
func (rt *runtime) usage(public []*registry.Descriptor, reason string) {
	fmt.Fprintln(rt.stderr, reason)
	fmt.Fprintln(rt.stderr, Tr("usage: <binary> <applet> [arguments]"))
	if len(public) > 0 {
		fmt.Fprintln(rt.stderr, Tr("applets:"))
		for _, d := range public {
			fmt.Fprintf(rt.stderr, "  %s\n", primaryAlias(d))
		}
	}
}

// invocationPlan carries the products of the side-effect-free planning
// steps of the pipeline: sources, loaded files, the effective core
// config, controls, the resolved composition and the strict schema —
// everything up to (and excluding) ejection and the strict parse. Both
// the real run and Introspector.Arguments consume it, so introspection
// truth cannot drift from execution truth.
type invocationPlan struct {
	src   conf.Sources
	files *conf.Files
	core  conf.Core
	ctl   graph.Controls
	res   graph.Result
	sch   *conf.Schema
}

// sections maps a resolved closure to config sections: the primary
// alias names each section and the metadata assertion happens here —
// the config engine never sees a descriptor.
func sections(ordered []graph.Member) []conf.Section {
	var out []conf.Section
	for _, m := range ordered {
		if m.Desc.ConfigPtr != nil {
			meta, _ := m.Desc.Metadata.(*conf.Meta)
			out = append(out, conf.Section{Name: primaryAlias(m.Desc), Ptr: m.Desc.ConfigPtr, Meta: meta})
		}
	}
	return out
}

// plan runs the pipeline's planning steps: lenient core peek (honoring
// an in-line --config), file loading, core refill, controls, closure
// resolution (with the console fallback), schema construction. It
// records violations into c and performs no side effects: nothing is
// written, ejected, injected, configured or started.
func (rt *runtime) plan(c *fail.Collector, d *registry.Descriptor, args []string) *invocationPlan {
	// the operator surfaces — env prefix, config file names and
	// sections, help — speak the applet's primary alias; the graph and
	// the inject vocabulary speak its id
	alias := primaryAlias(d)
	p := &invocationPlan{}
	p.src = conf.Sources{
		Args:         args,
		LookupEnv:    rt.lookupEnv,
		Locations:    rt.locations(alias),
		Stat:         rt.stat,
		Lstat:        rt.lstat,
		Open:         rt.open,
		OpenPinned:   rt.openPinned,
		Providers:    rt.providers(),
		SuppressCore: rt.suppressed,
		MaxSize:      rt.maxConfig,
	}
	before := c.Len()
	peek := conf.PeekCore(c, alias, p.src)
	if c.Len() == before {
		p.files = conf.LoadFiles(c, p.src, rt.explicitPath(peek))
	}
	if c.Len() == before {
		p.core = p.files.ApplyCore(c, alias, p.src)
		p.ctl = rt.controls(c, p.core)
	}
	if c.Len() == before {
		root := rt.coreRoot(c, d, rt.providerSeeds(p.files))
		if contains(p.ctl.Disable, d.ID) {
			// as a required dependency of the core node the applet
			// would fail resolution anyway; this keeps the message
			// human
			c.Fail("applet %q is disabled", alias)
		}
		if c.Len() == before {
			p.res = graph.Resolve(c, rt.reg, root, p.ctl)
		}
	}
	if c.Len() == before {
		p.sch = conf.NewSchema(c, alias, &p.core, sections(p.res.Ordered), rt.suppressed)
	}
	return p
}

// execute is the post-dispatch pipeline: planning, ejection, strict
// parse, then help/write-config short-circuits or the lifecycle.
func (rt *runtime) execute(buffer *logging.Buffer, d *registry.Descriptor, applet Applet, args []string) int {
	code := 2
	p := rt.plan(rt.c, d, args)
	if rt.c.Len() == 0 {
		keep := map[string]bool{}
		for _, m := range p.res.Ordered {
			keep[m.Desc.ID] = true
		}
		// an introspecting closure keeps the whole registry:
		// enumerating the binary is the point
		if !keep[IntrospectionID] {
			rt.reg.Retain(keep)
		}
		loaded := p.sch.Apply(rt.c, p.files, p.src)
		if rt.c.Len() == 0 {
			positionals = loaded.Positionals
			pre := rt.prepareTranslator(p.res)
			if p.core.Help {
				code = rt.help(p.sch)
			} else if p.core.WriteConfig {
				code = rt.writeConfig(p.sch, p.core.Config, p.src)
			} else {
				code = rt.lifecycle(buffer, p.res, applet, pre)
			}
		}
	}
	if rt.c.Len() > 0 {
		code = rt.report(buffer)
	}
	return code
}

// prepared records the outcome of the translator-first Configured
// pass: members already injected and configured (skipped by the main
// lifecycle's corresponding passes), the degraded translator itself
// (skipped entirely — an unconfigured translator must not be
// started), and the recorded error of a failed subtree DEPENDENCY —
// replayed fatally by the lifecycle without invoking the service's
// Configured a second time: Configured is called once, even on the
// failure path.
type prepared struct {
	injected   map[string]bool
	configured map[string]bool
	skip       map[string]bool
	depID      string
	depErr     error
}

func (p *prepared) injectedSet() map[string]bool {
	var out map[string]bool
	if p != nil {
		out = p.injected
	}
	return out
}

func (p *prepared) isConfigured(id string) bool {
	return p != nil && p.configured[id]
}

func (p *prepared) isSkipped(id string) bool {
	return p != nil && p.skip[id]
}

// prepareTranslator runs Inject + Configured over the registered
// Translator's dependency subtree before anything renders — on the
// help/write-config short-circuits this is the only lifecycle that
// happens (spec §7). The subtree is a query against the one
// resolution (the bindings are the edges); nothing resolves twice.
// The translator's own failure degrades quietly: one buffered
// warning, raw msgids, never a failed startup. A failing subtree
// DEPENDENCY records its error instead — the run path surfaces it
// fatally under the normal rules, the short-circuit paths render
// untranslated; only translation degrades silently, not services.
func (rt *runtime) prepareTranslator(res graph.Result) *prepared {
	var out *prepared
	if rt.translatorID != "" {
		if sub, member := res.Subtree(rt.translatorID); member {
			subC := &fail.Collector{}
			sub.Inject(subC)
			if subC.Len() == 0 {
				out = &prepared{injected: map[string]bool{}, configured: map[string]bool{}, skip: map[string]bool{}}
				for _, m := range sub.Ordered {
					out.injected[m.Desc.ID] = true
				}
				ok := true
				for i := 0; i < len(sub.Ordered) && ok; i++ {
					m := sub.Ordered[i]
					if c, isConfigurable := m.Desc.Instance.(Configurable); isConfigurable {
						if err := c.Configured(); err == nil {
							out.configured[m.Desc.ID] = true
						} else {
							ok = false
							out.skip[m.Desc.ID] = true
							if m.Desc.ID == rt.translatorID {
								slog.Warn("translator unavailable, proceeding untranslated", "service", m.Desc.ID, "error", err)
							} else {
								out.depID, out.depErr = m.Desc.ID, err
							}
						}
					}
				}
				if ok {
					for _, m := range sub.Ordered {
						if m.Desc.ID == rt.translatorID {
							if tr, isTranslator := m.Desc.Instance.(Translator); isTranslator {
								activeTranslator = tr
							}
						}
					}
				}
			} else {
				slog.Warn("translator subtree injection failed, proceeding untranslated", "service", rt.translatorID)
			}
		}
	}
	return out
}

// lifecycle drives inject → Configured → log swap → Start → applet →
// reverse Stop. Failures before the swap are collected and reported
// with the buffered logs; failures after it are logged live. Members
// the translator-first pass already configured are not re-Configured;
// a degraded translator is skipped entirely.
func (rt *runtime) lifecycle(buffer *logging.Buffer, res graph.Result, applet Applet, pre *prepared) int {
	code := 2
	res.InjectExcept(rt.c, pre.injectedSet())
	if rt.c.Len() == 0 {
		configured := true
		if pre != nil && pre.depErr != nil {
			// a translator-subtree dependency failed in the early
			// pass; surface it under the normal fatal rules without a
			// second Configured call
			rt.c.Fail("service %q: %v", pre.depID, pre.depErr)
			configured = false
		}
		for i := 0; i < len(res.Ordered) && configured; i++ {
			id := res.Ordered[i].Desc.ID
			if c, ok := res.Ordered[i].Desc.Instance.(Configurable); ok && !pre.isConfigured(id) && !pre.isSkipped(id) {
				if err := c.Configured(); err != nil {
					rt.c.Fail("service %q: %v", id, err)
					configured = false
				}
			}
		}
		if configured {
			for _, cycle := range res.Cycles {
				slog.Warn("dependency cycle detected: the start-order promise is weakened inside it", "cycle", cycle)
			}
			for _, from := range res.UnusedOverrides {
				slog.Warn("override matched no dependency", "from", from)
			}
			multi := rt.assembleSinks(res)
			if err := buffer.Replay(multi); err != nil {
				fmt.Fprintf(rt.stderr, "log replay: %v\n", err)
			}
			slog.SetDefault(slog.New(multi))
			var started []*graph.Member
			healthy := true
			for i := 0; i < len(res.Ordered) && healthy; i++ {
				if s, ok := res.Ordered[i].Desc.Instance.(Starter); ok && !pre.isSkipped(res.Ordered[i].Desc.ID) {
					if err := s.Start(); err == nil {
						started = append(started, &res.Ordered[i])
					} else {
						slog.Error("service start failed", "service", res.Ordered[i].Desc.ID, "error", err)
						healthy = false
					}
				}
			}
			if healthy {
				code = rt.runApplet(applet)
			}
			for i := len(started) - 1; i >= 0; i-- {
				if err := started[i].Desc.Instance.(Stopper).Stop(); err != nil {
					slog.Error("service stop failed", "service", started[i].Desc.ID, "error", err)
				}
			}
		}
	}
	return code
}

func (rt *runtime) runApplet(applet Applet) int {
	var code int
	if rt.execApplet != nil {
		code = rt.execApplet(applet)
	} else {
		code = applet.Run()
	}
	return code
}

// assembleSinks builds the multihandler over the closure's sinks in
// start order. A closure with no sink falls to the last-resort raw
// stderr handler — the framework's unconditional logging floor. There
// is no silence switch: a binary that wants no output redirects stderr
// itself. Richer logging is opt-in — the console sink (or any other)
// is enabled with --enable or pulled by a genuine dependency.
func (rt *runtime) assembleSinks(res graph.Result) *logging.Multi {
	var sinks []slog.Handler
	for _, m := range res.Ordered {
		if providesType(m.Desc, handlerType) {
			if h, ok := m.Desc.Instance.(slog.Handler); ok {
				sinks = append(sinks, h)
			}
		}
	}
	if len(sinks) == 0 {
		sinks = append(sinks, slog.NewTextHandler(rt.stderr, nil))
	}
	return logging.NewMulti(sinks...)
}

// report prints every collected violation and flushes the buffered
// startup logs to stderr, once.
func (rt *runtime) report(buffer *logging.Buffer) int {
	if !rt.reported {
		rt.reported = true
		for _, err := range rt.c.All() {
			fmt.Fprintf(rt.stderr, "error: %v\n", err)
		}
		if buffer.Len() > 0 {
			buffer.Replay(slog.NewTextHandler(rt.stderr, nil))
		}
	}
	return 2
}

// explicitPath resolves the --config path of this run. In write-config
// mode the target is input and output both: an existing target is
// loaded (normalizing an existing file), a missing one is only created.
func (rt *runtime) explicitPath(peek conf.Core) string {
	out := peek.Config
	if peek.WriteConfig && out != "" {
		if _, err := rt.stat(out); err != nil {
			out = ""
		}
	}
	return out
}

// controls translates the operator's service references into graph
// identities: disable/enable/override values accept BOTH vocabularies
// — aliases (what operators speak) and ids (what inject tags and docs
// say). The graph stays identity-based and ignorant of aliases.
// Override's from side is special: it matches dependency REFERENCES
// (tag strings), which may name nothing registered — an unresolvable
// from stays raw and at worst earns the unused-override warning.
func (rt *runtime) controls(c *fail.Collector, core conf.Core) graph.Controls {
	ctl := graph.Controls{}
	for _, ref := range core.Disable {
		if d, ok := rt.resolveRef(c, ref); ok {
			ctl.Disable = append(ctl.Disable, d.ID)
		} else {
			c.Fail("disable: unknown service %q", ref)
		}
	}
	for _, ref := range core.Enable {
		if d, ok := rt.resolveRef(c, ref); ok {
			ctl.Enable = append(ctl.Enable, d.ID)
		} else {
			c.Fail("enable: unknown service %q", ref)
		}
	}
	for _, entry := range core.Override {
		from, to, wellFormed := strings.Cut(entry, "=")
		if wellFormed && from != "" && to != "" {
			if ctl.Override == nil {
				ctl.Override = map[string]string{}
			}
			if fromD, ok := rt.resolveRef(c, from); ok {
				from = fromD.ID
			}
			if toD, ok := rt.resolveRef(c, to); ok {
				ctl.Override[from] = toD.ID
			} else {
				c.Fail("override: unknown substitute %q for %q", to, from)
			}
		} else {
			c.Fail("override %q: expected from=to", entry)
		}
	}
	return ctl
}

// providers returns every registered service declaring
// ConfigFormatProvider, in registration order.
func (rt *runtime) providers() []conf.Provider {
	var out []conf.Provider
	for _, d := range rt.reg.All() {
		if providesType(d, providerType) {
			if p, ok := d.Instance.(conf.Provider); ok {
				out = append(out, p)
			}
		}
	}
	return out
}

// providerSeeds maps the format providers that actually transcoded a
// file back to their service ids, so they join the closure and survive
// ejection.
func (rt *runtime) providerSeeds(files *conf.Files) []string {
	var out []string
	for _, used := range files.Used {
		for _, d := range rt.reg.All() {
			if d.Instance == used {
				out = append(out, d.ID)
			}
		}
	}
	return out
}

// coreRoot composes the per-invocation core node (spec §5): the
// virtual root of every resolution, a dynamically built struct whose
// inject fields are the system's needs — the dispatched applet by id
// and concrete type, the Translator (optional: present means pulled,
// the exactly-one rule is checked at startup), and one optional field
// per format provider in use (optional preserves the old seed
// semantics: a --disable'd provider drops from the closure silently;
// its transcode work happened before resolution regardless). The
// registry builds the descriptor through its normal machinery but
// never stores it — see the spec for why the root cannot be a
// registry entry.
func (rt *runtime) coreRoot(c *fail.Collector, d *registry.Descriptor, providerIDs []string) *registry.Descriptor {
	var root *registry.Descriptor
	{
		fields := []reflect.StructField{
			{Name: "Applet", Type: d.Concrete, Tag: reflect.StructTag(`inject:"` + d.ID + `"`)},
			{Name: "Translator", Type: translatorType, Tag: `inject:";optional"`},
		}
		for i, id := range providerIDs {
			fields = append(fields, reflect.StructField{
				Name: fmt.Sprintf("Provider%d", i),
				Type: providerType,
				Tag:  reflect.StructTag(`inject:"` + id + `;optional"`),
			})
		}
		root = rt.reg.Virtual(CoreAlias, reflect.New(reflect.StructOf(fields)).Interface(), c)
	}
	return root
}

// help renders the dispatched applet's full argument schema, grouped by
// service id, and exits 0.
func (rt *runtime) help(sch *conf.Schema) int {
	for _, section := range sch.HelpSections() {
		fmt.Fprintf(rt.stdout, "%s:\n", section.ID)
		for _, f := range section.Fields {
			if f.Long != "" {
				line := "  --" + f.Long
				if f.Short != "" {
					line += ", -" + f.Short
				}
				fmt.Fprintln(rt.stdout, line)
				if f.Usage != "" {
					fmt.Fprintf(rt.stdout, "        %s\n", Tr(f.Usage))
				}
				fmt.Fprintf(rt.stdout, "        %s\n", Tr("env: {name}, value: {value}", "name", f.EnvName, "value", sch.Value(f)))
			}
		}
	}
	return 0
}

// writeConfig emits the merged configuration: to stdout as json with no
// target, else to the target in the format its extension names.
func (rt *runtime) writeConfig(sch *conf.Schema, target string, src conf.Sources) int {
	code := 2
	js, err := sch.MarshalIndent()
	if err == nil {
		if target == "" {
			fmt.Fprintln(rt.stdout, string(js))
			code = 0
		} else {
			var payload []byte
			if payload, err = rt.transcode(js, target, src); err == nil {
				if err = os.WriteFile(target, payload, 0o600); err == nil {
					code = 0
				}
			}
		}
	}
	if err != nil {
		rt.c.Fail("write-config: %v", err)
	}
	return code
}

// transcode converts the json dump to the target's format by extension.
func (rt *runtime) transcode(js []byte, target string, src conf.Sources) ([]byte, error) {
	out := js
	var err error
	ext := strings.TrimPrefix(filepath.Ext(target), ".")
	if ext != "json" {
		found := false
		for i := 0; i < len(src.Providers) && !found; i++ {
			for _, candidate := range src.Providers[i].Extensions() {
				if candidate == ext && !found {
					found = true
					var converted io.Reader
					if converted, err = src.Providers[i].FromJSON(bytes.NewReader(js)); err == nil {
						out, err = io.ReadAll(converted)
					}
				}
			}
		}
		if !found && err == nil {
			err = fmt.Errorf("no format provider handles extension %q", ext)
		}
	}
	return out, err
}

func providesType(d *registry.Descriptor, t reflect.Type) bool {
	out := false
	for _, it := range d.Provides {
		out = out || it == t
	}
	return out
}

// stripSCMDebug reports whether the vector carries the --scm-debug
// token (argv[0] is never a candidate) and returns it without the
// token.
func stripSCMDebug(argv []string) ([]string, bool) {
	var out []string
	found := false
	for i, arg := range argv {
		if i > 0 && arg == "--scm-debug" {
			found = true
		} else {
			out = append(out, arg)
		}
	}
	return out, found
}

func contains(list []string, want string) bool {
	out := false
	for _, entry := range list {
		out = out || entry == want
	}
	return out
}
