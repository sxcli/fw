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
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"sxcli.dev/fw/internal/config"
	"sxcli.dev/fw/internal/fail"
	"sxcli.dev/fw/internal/graph"
	"sxcli.dev/fw/internal/logging"
	"sxcli.dev/fw/internal/registry"
)

// Main runs the framework: dispatch, configuration, resolution,
// lifecycle, applet. It never returns. It takes no parameters by
// design — the argument vector is platform-sourced (POSIX: os.Args;
// Windows service mode: the vector the SCM hands to Execute).
func Main() {
	os.Exit(platformMain())
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
	// unless something injects it
	rt.reg.Register(introspectionID, &Introspector{rt: rt}, registry.Options{})
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
	if rt.c.Len() > 0 {
		rt.report(buffer)
	} else if appletID, applet, args, ok := rt.dispatch(); ok {
		code = rt.execute(buffer, appletID, applet, args)
	}
	return code
}

// dispatch picks the applet per the spec rules: single-applet mode
// (only non-System applets count, with the System-selector carve-out),
// else first-bare-argument selector (Hidden and System applets are
// selectable like any other), else basename(argv[0]) among non-Hidden
// applets.
func (rt *runtime) dispatch() (string, Applet, []string, bool) {
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
	id := ""
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
			if d, found := rt.reg.ByID(rest[0]); found && d.System {
				if a, isApplet := d.Instance.(Applet); isApplet {
					id, applet, args, ok = d.ID, a, rest[1:], true
				}
			}
		}
		if !ok {
			id, applet, args, ok = sole.ID, sole.Instance.(Applet), rest, true
		}
	} else if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		if d, found := rt.reg.ByID(rest[0]); found {
			if a, isApplet := d.Instance.(Applet); isApplet {
				id, applet, args, ok = d.ID, a, rest[1:], true
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
		if d, found := rt.reg.ByID(name); found && !d.Hidden {
			if a, isApplet := d.Instance.(Applet); isApplet {
				id, applet, args, ok = d.ID, a, rest, true
			}
		}
		if !ok {
			rt.usage(public, Tr("{name} does not name an applet", "name", name))
		}
	}
	return id, applet, args, ok
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
			fmt.Fprintf(rt.stderr, "  %s\n", d.ID)
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
	src     config.Sources
	files   *config.Files
	core    config.Core
	ctl     graph.Controls
	res     graph.Result
	members []*registry.Descriptor
	sch     *config.Schema
}

// plan runs the pipeline's planning steps: lenient core peek (honoring
// an in-line --config), file loading, core refill, controls, closure
// resolution (with the console fallback), schema construction. It
// records violations into c and performs no side effects: nothing is
// written, ejected, injected, configured or started.
func (rt *runtime) plan(c *fail.Collector, appletID string, args []string) *invocationPlan {
	p := &invocationPlan{}
	p.src = config.Sources{
		Args:         args,
		LookupEnv:    rt.lookupEnv,
		Locations:    rt.locations(appletID),
		Stat:         rt.stat,
		Lstat:        rt.lstat,
		Open:         rt.open,
		OpenPinned:   rt.openPinned,
		Providers:    rt.providers(),
		SuppressCore: rt.suppressed,
		MaxSize:      rt.maxConfig,
	}
	before := c.Len()
	peek := config.PeekCore(c, appletID, p.src)
	if c.Len() == before {
		p.files = config.LoadFiles(c, p.src, rt.explicitPath(peek))
	}
	if c.Len() == before {
		p.core = p.files.ApplyCore(c, appletID, p.src)
		p.ctl = rt.controls(c, p.core)
	}
	if c.Len() == before {
		seeds := append(rt.seedIDs(), rt.providerSeeds(p.files)...)
		p.res = graph.Resolve(c, rt.reg, appletID, seeds, p.ctl)
	}
	if c.Len() == before {
		for _, m := range p.res.Ordered {
			p.members = append(p.members, m.Desc)
		}
		p.sch = config.NewSchema(c, appletID, &p.core, p.members, rt.suppressed)
	}
	return p
}

// execute is the post-dispatch pipeline: planning, ejection, strict
// parse, then help/write-config short-circuits or the lifecycle.
func (rt *runtime) execute(buffer *logging.Buffer, appletID string, applet Applet, args []string) int {
	code := 2
	p := rt.plan(rt.c, appletID, args)
	if rt.c.Len() == 0 {
		keep := map[string]bool{}
		for _, m := range p.res.Ordered {
			keep[m.Desc.ID] = true
		}
		// an introspecting closure keeps the whole registry:
		// enumerating the binary is the point
		if !keep[introspectionID] {
			rt.reg.Retain(keep)
		}
		loaded := p.sch.Apply(rt.c, p.files, p.src)
		if rt.c.Len() == 0 {
			positionals = loaded.Positionals
			pre := rt.prepareTranslator(p.res, p.ctl)
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
// pass: members already configured (skipped by the lifecycle's own
// Configured loop) and the degraded translator itself (skipped
// entirely — an unconfigured translator must not be started).
type prepared struct {
	configured map[string]bool
	skip       map[string]bool
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
// happens (spec §7). The translator's own failure degrades quietly:
// one buffered warning, raw msgids, never a failed startup. A failing
// subtree DEPENDENCY is left unmarked — the main lifecycle re-runs
// its Configured and reports the failure under the normal fatal
// rules; only translation degrades silently, not services. The main
// Inject re-injects subtree members harmlessly (same instances into
// the same fields, resolved under the same controls).
func (rt *runtime) prepareTranslator(res graph.Result, ctl graph.Controls) *prepared {
	var out *prepared
	inClosure := false
	for _, m := range res.Ordered {
		if m.Desc.ID == rt.translatorID {
			inClosure = true
		}
	}
	if rt.translatorID != "" && inClosure {
		sub := &fail.Collector{}
		subRes := graph.Resolve(sub, rt.reg, rt.translatorID, nil, ctl)
		subRes.Inject(sub)
		if sub.Len() == 0 {
			out = &prepared{configured: map[string]bool{}, skip: map[string]bool{}}
			ok := true
			for i := 0; i < len(subRes.Ordered) && ok; i++ {
				m := subRes.Ordered[i]
				if c, isConfigurable := m.Desc.Instance.(Configurable); isConfigurable {
					if err := c.Configured(); err == nil {
						out.configured[m.Desc.ID] = true
					} else {
						ok = false
						if m.Desc.ID == rt.translatorID {
							slog.Warn("translator unavailable, proceeding untranslated", "service", m.Desc.ID, "error", err)
							out.skip[m.Desc.ID] = true
						}
					}
				}
			}
			if ok {
				for _, m := range subRes.Ordered {
					if m.Desc.ID == rt.translatorID {
						if tr, isTranslator := m.Desc.Instance.(Translator); isTranslator {
							activeTranslator = tr
						}
					}
				}
			}
		} else {
			slog.Warn("translator subtree unresolvable, proceeding untranslated", "service", rt.translatorID)
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
	res.Inject(rt.c)
	if rt.c.Len() == 0 {
		configured := true
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
func (rt *runtime) explicitPath(peek config.Core) string {
	out := peek.Config
	if peek.WriteConfig && out != "" {
		if _, err := rt.stat(out); err != nil {
			out = ""
		}
	}
	return out
}

// controls builds the resolver controls from the core config; override
// entries use the from=to form.
func (rt *runtime) controls(c *fail.Collector, core config.Core) graph.Controls {
	ctl := graph.Controls{Disable: core.Disable, Enable: core.Enable}
	for _, entry := range core.Override {
		from, to, wellFormed := strings.Cut(entry, "=")
		if wellFormed && from != "" && to != "" {
			if ctl.Override == nil {
				ctl.Override = map[string]string{}
			}
			ctl.Override[from] = to
		} else {
			c.Fail("override %q: expected from=to", entry)
		}
	}
	return ctl
}

// providers returns every registered service declaring
// ConfigFormatProvider, in registration order.
func (rt *runtime) providers() []config.Provider {
	var out []config.Provider
	for _, d := range rt.reg.All() {
		if providesType(d, providerType) {
			if p, ok := d.Instance.(config.Provider); ok {
				out = append(out, p)
			}
		}
	}
	return out
}

// providerSeeds maps the format providers that actually transcoded a
// file back to their service ids, so they join the closure and survive
// ejection.
func (rt *runtime) providerSeeds(files *config.Files) []string {
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

// seedIDs returns the closure seeds every invocation gets beyond the
// applet: the registered Translator, the core's own dependency (spec
// §7). Format providers in use are seeded separately in plan().
func (rt *runtime) seedIDs() []string {
	var out []string
	if rt.translatorID != "" {
		out = append(out, rt.translatorID)
	}
	return out
}

// help renders the dispatched applet's full argument schema, grouped by
// service id, and exits 0.
func (rt *runtime) help(sch *config.Schema) int {
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
func (rt *runtime) writeConfig(sch *config.Schema, target string, src config.Sources) int {
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
func (rt *runtime) transcode(js []byte, target string, src config.Sources) ([]byte, error) {
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
