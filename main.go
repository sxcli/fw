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
	"fmt"
	"log/slog"
	"reflect"
	"strings"

	"sxcli.dev/fw/system"

	"sxcli.dev/conf/engine"
	"sxcli.dev/conf/fail"
	"sxcli.dev/fw/internal/ctlhook"
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
	rt.index(rt.c)
	// the system service was cataloged by init like any member; only
	// its attachment is ours — same package, unexported field, no
	// public seam. The catalog SNAPSHOT is taken here, before any
	// ejection: every later introspection view answers from it, so
	// ejection stays uniform and cannot blind the system service.
	if d, ok := rt.reg.ByID(system.ID); ok {
		if s, isOurs := d.Instance.(*systemService); isOurs {
			s.cat = rt.catalog.snapshot()
		}
	}

	if rt.c.Len() > 0 {
		rt.report(buffer)
	} else if rt.appletsRequested() {
		if rt.isRoot() {
			// only --help serves as root: the listing has no applet
			// to vouch for the run
			rt.c.Fail("--applets: running as root is not supported")
			code = rt.report(buffer)
		} else {
			// the listing serves before dispatch: `mybox --applets`
			// has no selector word, so dispatch would fail exactly
			// when the listing is wanted most
			code = rt.listApplets()
		}
	} else if d, applet, args, ok := rt.dispatch(); ok {
		code = rt.execute(buffer, d, applet, args)
	}
	return code
}

// isRoot answers the platform's superuser truth; test worlds that
// never set the seam are not root.
func (rt *runtime) isRoot() bool {
	return rt.superuser != nil && rt.superuser()
}

// appletsRequested scans argv for the --applets core argument — a
// bare `--` ends the scan, and suppressing FeatureApplets turns the
// token back into an unknown argument for whatever runs.
func (rt *runtime) appletsRequested() bool {
	if contains(rt.suppressed, "applets") {
		return false
	}
	for i := 1; i < len(rt.argv); i++ {
		if rt.argv[i] == "--" {
			break
		}
		if rt.argv[i] == "--applets" {
			return true
		}
	}
	return false
}

// listApplets prints the binary's public applets from the catalog —
// no resolution, no config load, no working set: the listing is a
// catalog question and this is its door. Composed order, the same
// Hidden filter as Introspector.Applets.
func (rt *runtime) listApplets() int {
	all := rt.reg.All()
	for i := 0; i < len(all); i++ {
		if all[i].Applet && !all[i].Hidden {
			line := all[i].Alias
			if meta, has := all[i].Metadata.(*engine.Meta); has && meta.Description != "" {
				line += " — " + firstLine(meta.Description)
			}
			fmt.Fprintln(rt.stdout, line)
		}
	}
	return 0
}

// firstLine cuts a description at its first newline: the listing is
// one line per applet.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
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
			name = BinaryBasename(rt.argv[0])
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
		fmt.Fprintln(rt.stderr, Tr("run with --applets to list the applets"))
	}
}

// invocationPlan carries the products of the side-effect-free planning
// steps of the pipeline: sources, loaded files, the effective core
// config, controls, the resolved composition and the strict schema —
// everything up to (and excluding) ejection and the strict parse. Both
// the real run and Introspector.Arguments consume it, so introspection
// truth cannot drift from execution truth.
type invocationPlan struct {
	src   engine.Sources
	files *engine.Files
	core  engine.Core
	ctrl  any // control knobs from the hook; nil without the import
	kn    upgradeKnobs
	ls    listingKnob
	ctl   graph.Controls
	res   graph.Result
	sch   *engine.Schema

	// peek-authoritative serve requests: all argument-only, so the
	// lenient peek is their source of truth even when later stages die
	help     bool
	validate bool
	upgrade  bool
	target   string // --config at peek time, the upgrade target
}

// listingKnob is the --applets argument's schema surface; the scan
// that serves it reads argv directly, pre-dispatch.
type listingKnob struct {
	Applets bool `json:"applets" conf:"applets" env:"-" dump:"-" usage:"list the binary's applets and exit"`
}

// upgradeKnobs is the framework's serving of the --upgrade-config
// tool flags: run-scoped, argument-only, mirrored from the front
// door's — each door owns its surface.
type upgradeKnobs struct {
	UpgradeConfig bool     `json:"upgradeConfig" conf:"upgrade-config" dump:"-" env:"-" usage:"migrate the --config file's sections to their current schema versions and exit"`
	FromVersion   []string `json:"fromVersion" conf:"from-version" dump:"-" env:"-" usage:"assert a versionless section's version, section=N (bare N when only one section qualifies)"`
}

// coreContribs assembles the composite core: the engine's knobs
// first (its short forms win), then the framework's tool knobs, the
// listing knob and — only in a binary that imported
// sxcli.dev/fw/controls — the service controls. A nil contribution
// pointer contributes nothing, so the absent case needs no branch.
func coreContribs(core *engine.Core, ctl any, kn *upgradeKnobs, ls *listingKnob) []engine.Contribution {
	meta := (*engine.Meta)(nil)
	if ctlhook.Registered != nil {
		meta = ctlhook.Registered.Meta
	}
	return []engine.Contribution{engine.CoreContrib(core), {Ptr: ctl, Meta: meta}, {Ptr: kn}, {Ptr: ls}}
}

// newControlKnobs returns a fresh controls contribution, nil when the
// binary never imported the controls package.
func newControlKnobs() any {
	if ctlhook.Registered != nil {
		return ctlhook.Registered.New()
	}
	return nil
}

// translateControls hands filled control knobs to the registered
// implementation; without one the graph gets no controls, ever.
func (rt *runtime) translateControls(c *fail.Collector, k any) graph.Controls {
	if ctlhook.Registered == nil || k == nil {
		return graph.Controls{}
	}
	return ctlhook.Registered.Translate(c, wsView{rt}, k)
}

// wsView adapts the working set to the controls' two-dictionary view.
type wsView struct{ rt *runtime }

func (v wsView) ByAlias(name string) (ctlhook.Ref, bool) {
	d, ok := v.rt.wsByAlias[name]
	if !ok {
		return ctlhook.Ref{}, false
	}
	return ctlhook.Ref{ID: d.ID, Applet: d.Applet, Core: d.Core}, true
}

func (v wsView) ByID(id string) (ctlhook.Ref, bool) {
	d, ok := v.rt.ws.ByID(id)
	if !ok {
		return ctlhook.Ref{}, false
	}
	return ctlhook.Ref{ID: d.ID, Applet: d.Applet, Core: d.Core}, true
}

// sections maps a resolved service set to config sections: the
// primary
// alias names each section and the metadata assertion happens here —
// the config engine never sees a descriptor.
func sections(ordered []graph.Member) []engine.Section {
	var out []engine.Section
	for _, m := range ordered {
		if m.Desc.ConfigPtr != nil {
			meta, _ := m.Desc.Metadata.(*engine.Meta)
			steps, _ := m.Desc.Migrations.([]engine.Step)
			out = append(out, engine.Section{Name: m.Desc.Alias, ID: m.Desc.ID, Ptr: m.Desc.ConfigPtr, Meta: meta, Steps: steps})
		}
	}
	return out
}

// plan runs the pipeline's planning steps: lenient core peek (honoring
// an in-line --config), file loading, core refill, controls,
// resolved-service-set resolution (with the console fallback),
// schema construction. It
// records violations into c and performs no side effects: nothing is
// written, ejected, injected, configured or started.
func (rt *runtime) plan(c *fail.Collector, d *registry.Descriptor, args []string) *invocationPlan {
	// the operator surfaces — env prefix, config file names and
	// sections, help — speak the applet's primary alias; the graph and
	// the inject vocabulary speak its id
	alias := d.Alias
	p := &invocationPlan{}
	p.src = engine.Sources{
		Args:           args,
		LookupEnv:      rt.lookupEnv,
		Locations:      rt.locations(alias),
		Stat:           rt.stat,
		Lstat:          rt.lstat,
		Open:           rt.open,
		OpenPinned:     rt.openPinned,
		Providers:      rt.providers(),
		SuppressCore:   rt.suppressed,
		ConfigMaxBytes: rt.configMaxBytes,
	}
	before := c.Len()
	var peek engine.Core
	// pre-seeded with the author's effective cap: the lenient parse
	// overwrites it only when the argument appears, so absent keeps
	// the author's value and an explicit 0 is the operator choosing
	// UNLIMITED
	peek.ConfigMaxBytes = rt.configMaxBytes
	peekCtrl := newControlKnobs()
	var peekKn upgradeKnobs
	var peekLs listingKnob
	engine.PeekCore(c, alias, p.src, coreContribs(&peek, peekCtrl, &peekKn, &peekLs))
	p.help = peek.Help
	p.validate = peek.ValidateConfig
	p.target = peek.Config
	p.src.ConfigMaxBytes = peek.ConfigMaxBytes
	if peekKn.UpgradeConfig && c.Len() == before {
		// the pure file transform never loads configuration; the rest
		// of the plan is not its business
		p.upgrade = true
		p.kn = peekKn
		return p
	}
	if c.Len() == before {
		p.files = engine.LoadFiles(c, p.src, rt.explicitPath(peek))
	}
	if c.Len() == before {
		p.ctrl = newControlKnobs()
		p.files.ApplyCore(c, alias, p.src, coreContribs(&p.core, p.ctrl, &p.kn, &p.ls))
		p.ctl = rt.translateControls(c, p.ctrl)
	}
	if c.Len() == before {
		root := rt.coreRoot(c, d, rt.providerSeeds(p.files))
		if c.Len() == before {
			p.res = graph.Resolve(c, rt.ws, root, p.ctl)
		}
		if c.Len() == before {
			// the backstop behind every door: resolution ran against
			// a working set holding one applet by construction, so
			// any other count means a door nobody imagined
			applets := 0
			for i := 0; i < len(p.res.Ordered); i++ {
				if p.res.Ordered[i].Desc.Applet {
					applets++
				}
			}
			if applets != 1 {
				c.Fail("internal: the resolved service set holds %d applets — exactly one is ever active", applets)
			}
		}
	}
	if c.Len() == before {
		p.sch = rt.schema(c, d, p.res, &p.core, p.ctrl, &p.kn, &p.ls)
	}
	return p
}

// execute is the post-dispatch pipeline: planning, ejection, strict
// parse, then help/write-config short-circuits or the lifecycle.
func (rt *runtime) execute(buffer *logging.Buffer, d *registry.Descriptor, applet Applet, args []string) int {
	code := 2
	rt.workingSet(d)
	p := rt.plan(rt.c, d, args)
	if rt.isRoot() && !d.AllowsSuperuser {
		if p.help && !p.upgrade {
			// help is the one door that still serves — an operator
			// staring at a refusing binary needs the page that
			// explains it
			fmt.Fprintln(rt.stderr, Tr("warning: running as root is not supported"))
		} else {
			rt.c.Fail("applet %q does not support running as root — the registration can declare AllowsSuperuser", d.Alias)
		}
	}
	if p.upgrade {
		if rt.c.Len() == 0 {
			rt.upgradeConfig(d, p)
		}
		if rt.c.Len() > 0 {
			return rt.report(buffer)
		}
		return 0
	}
	if rt.c.Len() == 0 {
		keep := map[string]bool{}
		for _, m := range p.res.Ordered {
			keep[m.Desc.ID] = true
		}
		// ejection is uniform: the system service answers from its
		// attach-time snapshot, so no resolved service set needs the
		// registry kept
		// alive on its behalf
		rt.ws.Retain(keep)
		loaded := p.sch.Apply(rt.c, p.files, p.src)
		if rt.c.Len() == 0 {
			// declared positionals were assigned by Apply; an
			// undeclared non-empty tail in the framework is a
			// violation — declare it or lose it, loudly
			if len(loaded.Positionals) > 0 {
				rt.c.Fail("unexpected argument %q", loaded.Positionals[0])
			}
		}
		if rt.c.Len() == 0 && p.validate {
			// load-but-never-run: every check passed, say nothing —
			// and NOTHING ran: not even the translator's Configured
			code = 0
		} else if rt.c.Len() == 0 {
			pre := rt.prepareTranslator(p.res)
			if p.help {
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
		if p.help {
			// best-effort by decree: the violations are reported, the
			// schema still renders, and the run counts as served
			rt.help(rt.helpSchema(d, p))
			code = 0
		}
	}
	return code
}

// helpSchema delivers the best schema a violated plan allows: the
// planned one when it exists (its values marked suspect where sources
// erred), else the registration-level fallback — resolved with empty
// controls, no sources applied, values showing factory defaults,
// built by THE schema builder like every other schema.
func (rt *runtime) helpSchema(d *registry.Descriptor, p *invocationPlan) *engine.Schema {
	if p.sch != nil {
		return p.sch
	}
	fallback := &fail.Collector{}
	var core engine.Core
	ctrl := newControlKnobs()
	var kn upgradeKnobs
	var ls listingKnob
	root := rt.coreRoot(fallback, d, nil)
	var res graph.Result
	if fallback.Len() == 0 {
		res = graph.Resolve(fallback, rt.reg, root, graph.Controls{})
	}
	return rt.schema(fallback, d, res, &core, ctrl, &kn, &ls)
}

// upgradeConfig serves --upgrade-config: the schema is built from the
// WHOLE catalog — the file being transformed serves the whole binary,
// not one applet's resolved service set — and the transform runs
// against the
// explicit --config target.
func (rt *runtime) upgradeConfig(d *registry.Descriptor, p *invocationPlan) {
	if p.target == "" {
		rt.c.Fail("upgrade-config requires an explicit --config target")
		return
	}
	from, bare := engine.ParseFromVersions(rt.c, p.kn.FromVersion)
	var core engine.Core
	ctrl := newControlKnobs()
	var kn upgradeKnobs
	var ls listingKnob
	var all []engine.Section
	for _, member := range rt.reg.All() {
		if member.ConfigPtr != nil {
			meta, _ := member.Metadata.(*engine.Meta)
			steps, _ := member.Migrations.([]engine.Step)
			all = append(all, engine.Section{Name: member.Alias, Ptr: member.ConfigPtr, Meta: meta, Steps: steps})
		}
	}
	// a FILE schema: sections, chains and fields only — argument and
	// environment uniqueness is scoped to the resolved service set by
	// spec and irrelevant to a file transform (two applets with
	// disjoint resolved service sets may both say conf:"port"; their
	// shared file must still upgrade)
	sch := engine.NewFileSchema(rt.c, d.Alias, coreContribs(&core, ctrl, &kn, &ls), all, rt.suppressed)
	if rt.c.Len() == 0 {
		sch.UpgradeFile(rt.c, p.target, from, bare, p.src)
	}
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

// assembleSinks builds the multihandler over the resolved service
// set's sinks in start order. A resolved service set with no sink
// falls to the last-resort raw
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
func (rt *runtime) explicitPath(peek engine.Core) string {
	out := peek.Config
	if peek.WriteConfig && out != "" {
		if _, err := rt.stat(out); err != nil {
			out = ""
		}
	}
	return out
}

// providers returns every registered service declaring
// ConfigFormatProvider, in registration order.
func (rt *runtime) providers() []engine.Provider {
	var out []engine.Provider
	for _, d := range rt.reg.All() {
		if providesType(d, providerType) {
			if p, ok := d.Instance.(engine.Provider); ok {
				out = append(out, p)
			}
		}
	}
	return out
}

// providerSeeds maps the format providers that actually transcoded a
// file back to their service ids, so they join the resolved service
// set and survive
// ejection.
func (rt *runtime) providerSeeds(files *engine.Files) []string {
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
// semantics: a --disable'd provider drops from the resolved service
// set silently;
// its transcode work happened before resolution regardless). The
// registry builds the descriptor through its normal machinery but
// never stores it — see the spec for why the root cannot be a
// registry entry.
func (ca *catalog) coreRoot(c *fail.Collector, d *registry.Descriptor, providerIDs []string) *registry.Descriptor {
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
		root = ca.reg.Virtual(CoreAlias, reflect.New(reflect.StructOf(fields)).Interface(), c)
	}
	return root
}

// help renders the dispatched applet's full argument schema, grouped by
// service id, and exits 0.
func (rt *runtime) help(sch *engine.Schema) int {
	indexed, rest := sch.PositionalFields()
	if len(indexed) > 0 || rest != nil {
		fmt.Fprintln(rt.stdout, "positionals:")
		for _, f := range indexed {
			line := "  <" + f.JSONPath[len(f.JSONPath)-1] + ">"
			fmt.Fprintln(rt.stdout, line)
			if f.Usage != "" {
				fmt.Fprintf(rt.stdout, "        %s\n", Tr(f.Usage))
			}
		}
		if rest != nil {
			fmt.Fprintf(rt.stdout, "  <%s...>\n", rest.JSONPath[len(rest.JSONPath)-1])
			if rest.Usage != "" {
				fmt.Fprintf(rt.stdout, "        %s\n", Tr(rest.Usage))
			}
		}
	}
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

// writeConfig emits the merged configuration through the engine,
// recording a failure as a startup violation.
func (rt *runtime) writeConfig(sch *engine.Schema, target string, src engine.Sources) int {
	code := 0
	if err := sch.WriteMerged(rt.stdout, target, src); err != nil {
		rt.c.Fail("write-config: %v", err)
		code = 2
	}
	return code
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
