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

	"github.com/sxcli/sxcli-fw/internal/config"
	"github.com/sxcli/sxcli-fw/internal/graph"
	"github.com/sxcli/sxcli-fw/internal/logging"
	"github.com/sxcli/sxcli-fw/internal/registry"
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

var alwaysOnType = reflect.TypeOf((*AlwaysOn)(nil)).Elem()
var handlerType = reflect.TypeOf((*slog.Handler)(nil)).Elem()
var providerType = reflect.TypeOf((*ConfigFormatProvider)(nil)).Elem()

// run is the whole pipeline; Main wraps it with the process exit.
// Framework-level failures exit 2; otherwise the exit code is the
// applet's.
func run(rt *runtime) int {
	code := 2
	previous := slog.Default()
	defer slog.SetDefault(previous)
	buffer := logging.NewBuffer()
	slog.SetDefault(slog.New(buffer))
	if rt.c.Len() > 0 {
		rt.report(buffer)
	} else if appletID, applet, args, ok := rt.dispatch(); ok {
		code = rt.execute(buffer, appletID, applet, args)
	}
	return code
}

// dispatch picks the applet per the spec rules: single-applet mode,
// else first-bare-argument selector, else basename(argv[0]).
func (rt *runtime) dispatch() (string, Applet, []string, bool) {
	var applets []*registry.Descriptor
	for _, d := range rt.reg.All() {
		if _, isApplet := d.Instance.(Applet); isApplet {
			applets = append(applets, d)
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
		rt.usage(applets, Tr("no applets are registered in this binary"))
	} else if len(applets) == 1 {
		id, applet, args, ok = applets[0].ID, applets[0].Instance.(Applet), rest, true
	} else if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		if d, found := rt.reg.ByID(rest[0]); found {
			if a, isApplet := d.Instance.(Applet); isApplet {
				id, applet, args, ok = d.ID, a, rest[1:], true
			}
		}
		if !ok {
			rt.usage(applets, Tr("{name} does not name an applet", "name", rest[0]))
		}
	} else {
		name := ""
		if len(rt.argv) > 0 {
			name = binaryBasename(rt.argv[0])
		}
		if d, found := rt.reg.ByID(name); found {
			if a, isApplet := d.Instance.(Applet); isApplet {
				id, applet, args, ok = d.ID, a, rest, true
			}
		}
		if !ok {
			rt.usage(applets, Tr("{name} does not name an applet", "name", name))
		}
	}
	return id, applet, args, ok
}

// usage prints the dispatch-failure usage to stderr; in single-applet
// mode the applet list is dropped.
func (rt *runtime) usage(applets []*registry.Descriptor, reason string) {
	fmt.Fprintln(rt.stderr, reason)
	fmt.Fprintln(rt.stderr, Tr("usage: <binary> <applet> [arguments]"))
	if len(applets) > 1 {
		fmt.Fprintln(rt.stderr, Tr("applets:"))
		for _, d := range applets {
			fmt.Fprintf(rt.stderr, "  %s\n", d.ID)
		}
	}
}

// execute is the post-dispatch pipeline: configuration, resolution,
// ejection, strict parse, then help/write-config short-circuits or the
// lifecycle.
func (rt *runtime) execute(buffer *logging.Buffer, appletID string, applet Applet, args []string) int {
	code := 2
	src := config.Sources{
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
	peek := config.PeekCore(rt.c, appletID, src)
	if rt.c.Len() == 0 {
		files := config.LoadFiles(rt.c, src, rt.explicitPath(peek))
		if rt.c.Len() == 0 {
			core := files.ApplyCore(rt.c, appletID, src)
			ctl := rt.controls(core)
			if rt.c.Len() == 0 {
				seeds := append(rt.alwaysOnIDs(), rt.providerSeeds(files)...)
				res := graph.Resolve(rt.c, rt.reg, appletID, seeds, ctl)
				if rt.c.Len() == 0 && !closureHasSink(res) {
					if _, registered := rt.reg.ByID("console"); registered && !contains(core.Disable, "console") {
						ctl.Enable = append(ctl.Enable, "console")
						res = graph.Resolve(rt.c, rt.reg, appletID, seeds, ctl)
					}
				}
				if rt.c.Len() == 0 {
					keep := map[string]bool{}
					var members []*registry.Descriptor
					for _, m := range res.Ordered {
						keep[m.Desc.ID] = true
						members = append(members, m.Desc)
					}
					rt.reg.Retain(keep)
					sch := config.NewSchema(rt.c, appletID, &core, members, rt.suppressed)
					loaded := sch.Apply(rt.c, files, src)
					if rt.c.Len() == 0 {
						positionals = loaded.Positionals
						if core.Help {
							code = rt.help(sch)
						} else if core.WriteConfig {
							code = rt.writeConfig(sch, core.Config, src)
						} else {
							code = rt.lifecycle(buffer, res, applet, contains(core.Disable, "console"))
						}
					}
				}
			}
		}
	}
	if rt.c.Len() > 0 {
		code = rt.report(buffer)
	}
	return code
}

// lifecycle drives inject → Configured → log swap → Start → applet →
// reverse Stop. Failures before the swap are collected and reported
// with the buffered logs; failures after it are logged live.
func (rt *runtime) lifecycle(buffer *logging.Buffer, res graph.Result, applet Applet, silence bool) int {
	code := 2
	res.Inject(rt.c)
	if rt.c.Len() == 0 {
		configured := true
		for i := 0; i < len(res.Ordered) && configured; i++ {
			if c, ok := res.Ordered[i].Desc.Instance.(Configurable); ok {
				if err := c.Configured(); err != nil {
					rt.c.Fail("service %q: %v", res.Ordered[i].Desc.ID, err)
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
			multi := rt.assembleSinks(res, silence)
			if err := buffer.Replay(multi); err != nil {
				fmt.Fprintf(rt.stderr, "log replay: %v\n", err)
			}
			slog.SetDefault(slog.New(multi))
			var started []*graph.Member
			healthy := true
			for i := 0; i < len(res.Ordered) && healthy; i++ {
				if s, ok := res.Ordered[i].Desc.Instance.(Starter); ok {
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
// start order. Zero sinks means: deliberate silence when the console
// was explicitly disabled, otherwise the last-resort stderr handler
// (console sink not linked into the binary).
func (rt *runtime) assembleSinks(res graph.Result, silence bool) *logging.Multi {
	var sinks []slog.Handler
	for _, m := range res.Ordered {
		if providesType(m.Desc, handlerType) {
			if h, ok := m.Desc.Instance.(slog.Handler); ok {
				sinks = append(sinks, h)
			}
		}
	}
	if len(sinks) == 0 && !silence {
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
func (rt *runtime) controls(core config.Core) graph.Controls {
	ctl := graph.Controls{Disable: core.Disable, Enable: core.Enable}
	for _, entry := range core.Override {
		from, to, wellFormed := strings.Cut(entry, "=")
		if wellFormed && from != "" && to != "" {
			if ctl.Override == nil {
				ctl.Override = map[string]string{}
			}
			ctl.Override[from] = to
		} else {
			rt.c.Fail("override %q: expected from=to", entry)
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

// alwaysOnIDs returns the ids of every service declaring AlwaysOn.
func (rt *runtime) alwaysOnIDs() []string {
	var out []string
	for _, d := range rt.reg.All() {
		if providesType(d, alwaysOnType) {
			out = append(out, d.ID)
		}
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

func closureHasSink(res graph.Result) bool {
	out := false
	for _, m := range res.Ordered {
		out = out || providesType(m.Desc, handlerType)
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
