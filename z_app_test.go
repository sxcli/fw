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
	"io"
	"io/fs"
	"strings"
	"sxcli.dev/fw/internal/ctlhook"
	"sxcli.dev/fw/system"
	"testing"

	"sxcli.dev/conf/engine"
	"sxcli.dev/conf/fail"
	"sxcli.dev/fw/internal/registry"
)

// The composition end-to-end: catalog chains → Build → the pipeline,
// with alias-shaped operator surfaces.

type appCfg struct {
	Version  uint32 `json:"version"`
	Greeting string `json:"greeting" conf:"greeting,g" usage:"the greeting"`
}

type appSrv struct {
	cfg appCfg
	log *[]string
}

func (a *appSrv) Configured() error { return nil }
func (a *appSrv) Run() int {
	*a.log = append(*a.log, "srv.run:"+a.cfg.Greeting)
	return 0
}

type appAux struct{ log *[]string }

func (a *appAux) Configured() error {
	*a.log = append(*a.log, "aux.configured")
	return nil
}

// appWorld composes an App from a private catalog and wires a
// runtime with hermetic seams around it.
func appWorld(t *testing.T, b *AppBuilder, argv []string, files, env map[string]string, register func(reg *registry.Registry, c *fail.Collector, log *[]string)) (*world, int) {
	return appWorldWith(t, b, argv, files, env, register, nil)
}

func appWorldWith(t *testing.T, b *AppBuilder, argv []string, files, env map[string]string, register func(reg *registry.Registry, c *fail.Collector, log *[]string), tweak func(*world)) (*world, int) {
	t.Helper()
	w := &world{c: &fail.Collector{}}
	reg, catalogC := catalogWorld()
	register(reg, catalogC, &w.log)
	// every binary carries the system service; app worlds are binaries
	if _, seeded := reg.ByID(system.ID); !seeded {
		NewBareRegistration(system.ID, func() *systemService { return &systemService{} }).
			Alias(SystemAlias).
			Provides(Iface[system.System]()).
			core().
			registerInto(reg, catalogC)
	}
	app, err := b.buildFrom(reg, catalogC)
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	w.rt = &runtime{
		catalog:        catalog{reg: app.reg, suppressed: suppressedCore, shortPriority: app.shortPriority},
		configMaxBytes: configMaxBytes,
		c:              w.c,
		argv:           argv,
		lookupEnv: func(name string) (string, bool) {
			v, ok := env[name]
			return v, ok
		},
		stdout: &w.stdout,
		stderr: &w.stderr,
		locations: func(alias string) []engine.Location {
			return []engine.Location{{Base: "/etc/" + alias + "/config"}}
		},
		stat: func(path string) (int64, error) {
			var size int64
			err := fs.ErrNotExist
			if content, ok := files[path]; ok {
				size = int64(len(content))
				err = nil
			}
			return size, err
		},
		open: func(path string) (io.ReadCloser, error) {
			var r io.ReadCloser
			err := fs.ErrNotExist
			if content, ok := files[path]; ok {
				r = io.NopCloser(strings.NewReader(content))
				err = nil
			}
			return r, err
		},
		openPinned: func(path string) (io.ReadCloser, error) { return nil, fs.ErrNotExist },
	}
	t.Cleanup(func() { activeTranslator = nil })
	if tweak != nil {
		tweak(w)
	}
	// the app is already built above; drive the pipeline directly
	return w, run(w.rt)
}

func registerSrv(alias string) func(reg *registry.Registry, c *fail.Collector, log *[]string) {
	return func(reg *registry.Registry, c *fail.Collector, log *[]string) {
		NewRegistration("example.com/app/srv", func() *appSrv { return &appSrv{log: log, cfg: appCfg{Version: 1, Greeting: "default"}} },
			func(s *appSrv) *appCfg { return &s.cfg }).
			Alias(alias).registerInto(reg, c)
	}
}

func TestAppAliasSurfaces(t *testing.T) {
	// args
	w, code := appWorld(t, Builder().AcceptAll(), []string{"bin", "--greeting=hi"}, nil, nil, registerSrv("srv"))
	if code != 0 || strings.Join(w.log, ",") != "srv.run:hi" {
		t.Errorf("arg surface wrong: code=%d log=%v stderr:\n%s", code, w.log, w.stderr.String())
	}
	// env: prefix from the ALIAS
	w, code = appWorld(t, Builder().AcceptAll(), []string{"bin"}, nil,
		map[string]string{"SRV__GREETING": "fromenv"}, registerSrv("srv"))
	if code != 0 || strings.Join(w.log, ",") != "srv.run:fromenv" {
		t.Errorf("env surface wrong: code=%d log=%v", code, w.log)
	}
	// config file: location AND section from the alias
	files := map[string]string{"/etc/srv/config.json": `{"srv": {"greeting": "fromfile"}}`}
	w, code = appWorld(t, Builder().AcceptAll(), []string{"bin"}, files, nil, registerSrv("srv"))
	if code != 0 || strings.Join(w.log, ",") != "srv.run:fromfile" {
		t.Errorf("file surface wrong: code=%d log=%v stderr:\n%s", code, w.log, w.stderr.String())
	}
}

func TestHyphenAliasReachesEnv(t *testing.T) {
	w, code := appWorld(t, Builder().AcceptAll(), []string{"bin"}, nil,
		map[string]string{"CHERRY_PICK__GREETING": "picked"}, registerSrv("cherry-pick"))
	if code != 0 || strings.Join(w.log, ",") != "srv.run:picked" {
		t.Errorf("hyphen alias env mapping wrong: code=%d log=%v stderr:\n%s", code, w.log, w.stderr.String())
	}
}

// TestSecondaryAliasSelects died with the single-alias model: a
// service has exactly ONE operator name and the composition renames
// it — secondary aliases no longer exist to select by. In its place:
// a second Alias call is the once-violation, judged by the shared
// rules at commit.
func TestAliasDeclaredOnce(t *testing.T) {
	reg, c := catalogWorld()
	NewBareRegistration("example.com/app/two", func() *appSrv2 { return &appSrv2{} }).
		Alias("two").Alias("dos").registerInto(reg, c)
	if c.Len() == 0 {
		t.Fatal("a second Alias call must be a violation")
	}
	if !strings.Contains(c.All()[0].Error(), `Alias called twice ("two"; then "dos")`) {
		t.Errorf("wrong verdict: %v", c.All())
	}
}

type appSrv2 struct{ log *[]string }

func (a *appSrv2) Configured() error { return nil }
func (a *appSrv2) Run() int          { *a.log = append(*a.log, "two.run"); return 0 }

// The applet listing left the usage text for --applets; usage now
// points there instead of embedding the directory.
func TestUsagePointsAtApplets(t *testing.T) {
	b := Builder().AcceptAll().Order("example.com/app/two", "example.com/app/srv")
	register := func(reg *registry.Registry, c *fail.Collector, log *[]string) {
		registerSrv("cherry-pick")(reg, c, log)
		NewBareRegistration("example.com/app/two", func() *appSrv2 { return &appSrv2{log: log} }).
			Alias("two").registerInto(reg, c)
	}
	w, code := appWorld(t, b, []string{"bin", "ghost"}, nil, nil, register)
	if code != 2 {
		t.Fatalf("dispatch failure expected, code=%d", code)
	}
	text := w.stderr.String()
	if !strings.Contains(text, "--applets") || strings.Contains(text, "cherry-pick") {
		t.Errorf("usage must point at --applets and embed no list: %s", text)
	}
}

type hidApplet struct{}

func (h *hidApplet) Configured() error { return nil }
func (h *hidApplet) Run() int          { return 0 }

func TestAppletsListing(t *testing.T) {
	b := Builder().AcceptAll().Order("example.com/app/two", "example.com/app/srv")
	register := func(reg *registry.Registry, c *fail.Collector, log *[]string) {
		registerSrv("cherry-pick")(reg, c, log)
		NewBareRegistration("example.com/app/two", func() *appSrv2 { return &appSrv2{log: log} }).
			Alias("two").
			Metadata(&Metadata{Description: "the second applet\nlong tail"}).
			registerInto(reg, c)
		NewBareRegistration("example.com/app/hid", func() *hidApplet { return &hidApplet{} }).
			Alias("hid").Hidden().registerInto(reg, c)
	}
	// pre-dispatch: no selector word, the listing still serves
	w, code := appWorld(t, b, []string{"bin", "--applets"}, nil, nil, register)
	if code != 0 {
		t.Fatalf("the listing serves pre-dispatch: code=%d\n%s", code, w.stderr.String())
	}
	out := w.stdout.String()
	if !strings.Contains(out, "two — the second applet") || !strings.Contains(out, "cherry-pick") {
		t.Errorf("the listing must carry aliases and first description lines: %q", out)
	}
	if strings.Contains(out, "long tail") || strings.Contains(out, "hid") {
		t.Errorf("hidden applets and description tails stay out: %q", out)
	}
	if strings.Index(out, "two") > strings.Index(out, "cherry-pick") {
		t.Errorf("the listing follows composed order: %q", out)
	}
	// a bare -- ends the scan: the token belongs to the applet
	w, code = appWorld(t, b, []string{"bin", "ghost", "--", "--applets"}, nil, nil, register)
	if code != 2 || strings.Contains(w.stdout.String(), "cherry-pick") {
		t.Errorf("past a bare -- the token is not the core argument: code=%d", code)
	}
	// suppressed: the scan never runs and the argument is unknown
	// like any other
	oldSup := suppressedCore
	t.Cleanup(func() { suppressedCore = oldSup })
	Suppress(FeatureApplets)
	w, code = appWorld(t, b, []string{"bin", "cherry-pick", "--applets"}, nil, nil, register)
	if code != 2 || !strings.Contains(w.stderr.String(), "unknown argument --applets") {
		t.Errorf("a suppressed listing must leave an unknown argument: code=%d\n%s", code, w.stderr.String())
	}
}

func TestControlsSpeakBothVocabularies(t *testing.T) {
	byAlias := func(reg *registry.Registry, c *fail.Collector, log *[]string) {
		registerSrv("srv")(reg, c, log)
		NewBareRegistration("example.com/app/aux", func() *appAux { return &appAux{log: log} }).
			Alias("aux").registerInto(reg, c)
	}
	w, code := appWorld(t, Builder().AcceptAll(), []string{"bin", "--enable", "aux"}, nil, nil, byAlias)
	if code != 0 || !strings.Contains(strings.Join(w.log, ","), "aux.configured") {
		t.Errorf("enable by alias failed: code=%d log=%v", code, w.log)
	}
	w, code = appWorld(t, Builder().AcceptAll(), []string{"bin", "--enable", "example.com/app/aux"}, nil, nil, byAlias)
	if code != 0 || !strings.Contains(strings.Join(w.log, ","), "aux.configured") {
		t.Errorf("enable by id failed: code=%d log=%v", code, w.log)
	}
	w, code = appWorld(t, Builder().AcceptAll(), []string{"bin", "--enable", "ghost"}, nil, nil, byAlias)
	if code != 2 || !strings.Contains(w.stderr.String(), `enable: unknown service "ghost"`) {
		t.Errorf("unknown ref must fail with the operator vocabulary: code=%d\n%s", code, w.stderr.String())
	}
}

func TestCoreFamilyIsNoControlTarget(t *testing.T) {
	register := func(reg *registry.Registry, c *fail.Collector, log *[]string) {
		registerSrv("srv")(reg, c, log)
	}
	want := "is a core service — the core family cannot be enabled, disabled or overridden"
	cases := [][]string{
		{"bin", "--disable", "system"},
		{"bin", "--enable", "system"},
		{"bin", "--override", "example.com/app/srv=system"},
		{"bin", "--override", "sxcli.dev/fw/system=srv"},
		{"bin", "--disable", "core"},
		{"bin", "--disable", "sxcli.dev/fw"},
		{"bin", "--enable", "core"},
		{"bin", "--override", "core=srv"},
	}
	for _, argv := range cases {
		w, code := appWorld(t, Builder().AcceptAll(), argv, nil, nil, register)
		if code != 2 || !strings.Contains(w.stderr.String(), want) {
			t.Errorf("%v must refuse with the core-family verdict: code=%d\n%s", argv, code, w.stderr.String())
		}
	}
}

func TestConfigMaxBytesOperatorOverride(t *testing.T) {
	register := func(reg *registry.Registry, c *fail.Collector, log *[]string) {
		registerSrv("srv")(reg, c, log)
	}
	pad := "{}" + strings.Repeat(" ", 100)
	files := map[string]string{"/etc/srv/config.json": pad}
	w, code := appWorld(t, Builder().AcceptAll(), []string{"bin"}, files, nil, register)
	if code != 0 {
		t.Fatalf("the padded file fits the default cap: code=%d\n%s", code, w.stderr.String())
	}
	w, code = appWorld(t, Builder().AcceptAll(), []string{"bin", "--config-max-bytes", "10"}, files, nil, register)
	if code != 2 || !strings.Contains(w.stderr.String(), "exceeds the 10 byte limit") {
		t.Errorf("the operator's cap must win: code=%d\n%s", code, w.stderr.String())
	}
	// a negative value cannot even parse into the unsigned knob
	w, code = appWorld(t, Builder().AcceptAll(), []string{"bin", "--config-max-bytes=-1"}, files, nil, register)
	if code != 2 || !strings.Contains(w.stderr.String(), `--config-max-bytes: invalid unsigned integer "-1"`) {
		t.Errorf("a negative cap must refuse loudly: code=%d\n%s", code, w.stderr.String())
	}
	// an explicit zero removes the cap for this run, beating the
	// author's own tighter setting
	oldCap := configMaxBytes
	t.Cleanup(func() { configMaxBytes = oldCap })
	ConfigMaxBytes(10)
	w, code = appWorld(t, Builder().AcceptAll(), []string{"bin", "--config-max-bytes=0"}, files, nil, register)
	if code != 0 {
		t.Errorf("zero must mean unlimited: code=%d\n%s", code, w.stderr.String())
	}
	// and without the override the author's tiny cap refuses the file
	w, code = appWorld(t, Builder().AcceptAll(), []string{"bin"}, files, nil, register)
	if code != 2 || !strings.Contains(w.stderr.String(), "exceeds the 10 byte limit") {
		t.Errorf("the author's cap must hold when not overridden: code=%d\n%s", code, w.stderr.String())
	}
}

type shortyACfg struct {
	Version uint32 `json:"version"`
	A       int    `json:"a" conf:"a-value,p"`
}

type shortyBCfg struct {
	Version uint32 `json:"version"`
	B       int    `json:"b" conf:"b-value,p"`
}

type shortyA struct{ cfg shortyACfg }

func (s *shortyA) Configured() error { return nil }

type shortyB struct{ cfg shortyBCfg }

func (s *shortyB) Configured() error { return nil }

// shortyApp pulls both contenders into its resolved service set.
type shortyApp struct {
	One *shortyA `inject:"example.com/app/one"`
	Two *shortyB `inject:"example.com/app/two"`
}

func (s *shortyApp) Configured() error { return nil }
func (s *shortyApp) Run() int          { return 0 }

func registerShorties(reg *registry.Registry, c *fail.Collector, log *[]string) {
	NewBareRegistration("example.com/app/shorty", func() *shortyApp { return &shortyApp{} }).
		Alias("shorty").registerInto(reg, c)
	NewRegistration("example.com/app/one", func() *shortyA { return &shortyA{cfg: shortyACfg{Version: 1}} },
		func(s *shortyA) *shortyACfg { return &s.cfg }).
		Alias("one").registerInto(reg, c)
	NewRegistration("example.com/app/two", func() *shortyB { return &shortyB{cfg: shortyBCfg{Version: 1}} },
		func(s *shortyB) *shortyBCfg { return &s.cfg }).
		Alias("two").registerInto(reg, c)
}

func TestShortArgPriorityEndToEnd(t *testing.T) {
	// unresolved contest: startup violation naming both services
	w, code := appWorld(t, Builder().AcceptAll(), []string{"bin"}, nil, nil, registerShorties)
	if code != 2 || !strings.Contains(w.stderr.String(), `short -p is contested by "example.com/app/one" and "example.com/app/two"`) {
		t.Errorf("an unresolved contest must refuse startup: code=%d\n%s", code, w.stderr.String())
	}
	// the composition's list resolves it; the loser is long-only
	b := Builder().AcceptAll().ShortArgPriority("example.com/app/two")
	w, code = appWorld(t, b, []string{"bin", "-p", "7"}, nil, nil, registerShorties)
	if code != 0 {
		t.Errorf("a listed winner resolves the contest: code=%d\n%s", code, w.stderr.String())
	}
	// a second call and an unknown id are Build violations
	reg, catalogC := catalogWorld()
	var log []string
	registerShorties(reg, catalogC, &log)
	_, err := Builder().AcceptAll().
		ShortArgPriority("example.com/app/two").
		ShortArgPriority("example.com/app/one").buildFrom(reg, catalogC)
	if err == nil || !strings.Contains(err.Error(), "ShortArgPriority called twice") {
		t.Errorf("the priority is declared once, atomically: %v", err)
	}
	reg, catalogC = catalogWorld()
	registerShorties(reg, catalogC, &log)
	_, err = Builder().AcceptAll().
		ShortArgPriority("example.com/app/ghost").buildFrom(reg, catalogC)
	if err == nil || !strings.Contains(err.Error(), `short-priority: unknown service id "example.com/app/ghost"`) {
		t.Errorf("an unknown id must be a violation: %v", err)
	}
}

// wantsIface requires a catIface provider — and in its world the
// only provider is a dormant applet, which the working set has
// already ejected.
type wantsIface struct {
	Dep catIface `inject:""`
}

func (w *wantsIface) Configured() error { return nil }
func (w *wantsIface) Run() int          { return 0 }

// provApplet is that dormant applet: it provides catIface, but a
// dormant applet is structurally invisible to resolution.
type provApplet struct{}

func (p *provApplet) Configured() error { return nil }
func (p *provApplet) Run() int          { return 0 }
func (p *provApplet) Cat()              {}

func TestDormantAppletsAreUnreachable(t *testing.T) {
	twoApplets := func(reg *registry.Registry, c *fail.Collector, log *[]string) {
		registerSrv("srv")(reg, c, log)
		NewBareRegistration("example.com/app/two", func() *appSrv2 { return &appSrv2{log: log} }).
			Alias("two").registerInto(reg, c)
	}
	// controls cannot name a dormant applet, by alias or id
	w, code := appWorld(t, Builder().AcceptAll(), []string{"bin", "srv", "--enable", "two"}, nil, nil, twoApplets)
	if code != 2 || !strings.Contains(w.stderr.String(), `enable: unknown service "two"`) {
		t.Errorf("a dormant applet must be unknown to controls: code=%d\n%s", code, w.stderr.String())
	}
	w, code = appWorld(t, Builder().AcceptAll(), []string{"bin", "srv", "--enable", "example.com/app/two"}, nil, nil, twoApplets)
	if code != 2 || !strings.Contains(w.stderr.String(), `enable: unknown service "example.com/app/two"`) {
		t.Errorf("a dormant applet id must be unknown to controls: code=%d\n%s", code, w.stderr.String())
	}
	w, code = appWorld(t, Builder().AcceptAll(), []string{"bin", "srv", "--override", "example.com/app/srv=two"}, nil, nil, twoApplets)
	if code != 2 || !strings.Contains(w.stderr.String(), `unknown`) {
		t.Errorf("an applet substitute must be unknown: code=%d\n%s", code, w.stderr.String())
	}
	// a dependency naming a dormant applet's id is unresolvable
	depOnApplet := func(reg *registry.Registry, c *fail.Collector, log *[]string) {
		NewBareRegistration("example.com/app/main", func() *appDepOnApplet { return &appDepOnApplet{} }).
			Alias("main").registerInto(reg, c)
		NewBareRegistration("example.com/app/two", func() *appSrv2 { return &appSrv2{log: log} }).
			Alias("two").registerInto(reg, c)
	}
	w, code = appWorld(t, Builder().AcceptAll(), []string{"bin", "main"}, nil, nil, depOnApplet)
	if code != 2 || !strings.Contains(w.stderr.String(), `unknown service id "example.com/app/two"`) {
		t.Errorf("a named-id dep on a dormant applet must be unresolvable: code=%d\n%s", code, w.stderr.String())
	}
	// an interface dep never sees a dormant applet as a candidate,
	// even when it is the only provider
	ifaceWorld := func(reg *registry.Registry, c *fail.Collector, log *[]string) {
		NewBareRegistration("example.com/app/wants", func() *wantsIface { return &wantsIface{} }).
			Alias("wants").registerInto(reg, c)
		NewBareRegistration("example.com/app/prov", func() *provApplet { return &provApplet{} }).
			Alias("prov").Provides(Iface[catIface]()).registerInto(reg, c)
	}
	w, code = appWorld(t, Builder().AcceptAll(), []string{"bin", "wants"}, nil, nil, ifaceWorld)
	if code != 2 || !strings.Contains(w.stderr.String(), "no registered service satisfies") {
		t.Errorf("an interface dep must not see a dormant applet: code=%d\n%s", code, w.stderr.String())
	}
}

type appDepOnApplet struct {
	Two *appSrv2 `inject:"example.com/app/two"`
}

func (a *appDepOnApplet) Configured() error { return nil }
func (a *appDepOnApplet) Run() int          { return 0 }

func TestOneAppletBackstop(t *testing.T) {
	depOnApplet := func(reg *registry.Registry, c *fail.Collector, log *[]string) {
		NewBareRegistration("example.com/app/main", func() *appDepOnApplet { return &appDepOnApplet{} }).
			Alias("main").registerInto(reg, c)
		NewBareRegistration("example.com/app/two", func() *appSrv2 { return &appSrv2{log: log} }).
			Alias("two").registerInto(reg, c)
	}
	// the normal run refuses at resolution: the dormant applet is not
	// in the working set, so the named-id dep is unresolvable
	w, code := appWorld(t, Builder().AcceptAll(), []string{"bin", "main"}, nil, nil, depOnApplet)
	if code != 2 {
		t.Fatalf("the dep on a dormant applet must refuse: code=%d", code)
	}
	// force a working set that kept both applets — the door nobody
	// imagined — and re-plan: resolution now pulls the second applet
	// in, and the backstop must refuse it
	w.rt.ws = w.rt.reg.Snapshot()
	w.rt.wsByAlias = w.rt.byAlias
	d, _ := w.rt.reg.ByID("example.com/app/main")
	c := &fail.Collector{}
	w.rt.plan(c, d, nil)
	all := errText2(c)
	if !strings.Contains(all, "exactly one is ever active") {
		t.Errorf("the backstop must catch a second applet: %v", c.All())
	}
}

func errText2(c *fail.Collector) string {
	var b strings.Builder
	for _, e := range c.All() {
		b.WriteString(e.Error())
		b.WriteString("|")
	}
	return b.String()
}

func TestControlsAbsentWithoutTheImport(t *testing.T) {
	// the test binary imports sxcli.dev/fw/controls, so the absence
	// is simulated by clearing the hook — the behavioral half of the
	// contract. The other half (the linker dropping the unlinked
	// package) is a build-graph fact no in-process test can see.
	old := ctlhook.Registered
	t.Cleanup(func() { ctlhook.Registered = old })
	ctlhook.Registered = nil
	w, code := appWorld(t, Builder().AcceptAll(), []string{"bin", "--disable", "srv"}, nil, nil, registerSrv("srv"))
	if code != 2 || !strings.Contains(w.stderr.String(), "unknown argument --disable") {
		t.Errorf("controls must not exist without the import: code=%d\n%s", code, w.stderr.String())
	}
	w, code = appWorld(t, Builder().AcceptAll(), []string{"bin", "--override", "a=b"}, nil, nil, registerSrv("srv"))
	if code != 2 || !strings.Contains(w.stderr.String(), "unknown argument --override") {
		t.Errorf("override must not exist without the import: code=%d\n%s", code, w.stderr.String())
	}
	// and Suppress of a control points at the import instead of
	// silently trimming nothing
	before := defaultCollector.Len()
	Suppress(FeatureOverride)
	if defaultCollector.Len() != before+1 {
		t.Error("suppressing an absent control must be a violation")
	}
}

func TestSuperuserGate(t *testing.T) {
	register := func(reg *registry.Registry, c *fail.Collector, log *[]string) {
		registerSrv("srv")(reg, c, log)
	}
	asRoot := func(w *world) { w.rt.superuser = func() bool { return true } }
	// execution refuses, naming the applet
	w, code := appWorldWith(t, Builder().AcceptAll(), []string{"bin"}, nil, nil, register, asRoot)
	if code != 2 || !strings.Contains(w.stderr.String(), `applet "srv" does not support running as root`) {
		t.Errorf("root must refuse without the declaration: code=%d\n%s", code, w.stderr.String())
	}
	// --help still serves, with the warning
	w, code = appWorldWith(t, Builder().AcceptAll(), []string{"bin", "--help"}, nil, nil, register, asRoot)
	if code != 0 || !strings.Contains(w.stderr.String(), "warning: running as root is not supported") {
		t.Errorf("help must serve with the warning: code=%d\n%s", code, w.stderr.String())
	}
	if !strings.Contains(w.stdout.String(), "--greeting") {
		t.Errorf("help must still render the schema: %q", w.stdout.String())
	}
	// --applets has no applet to vouch for the run
	w, code = appWorldWith(t, Builder().AcceptAll(), []string{"bin", "--applets"}, nil, nil, register, asRoot)
	if code != 2 || !strings.Contains(w.stderr.String(), "--applets: running as root is not supported") {
		t.Errorf("the listing must refuse as root: code=%d\n%s", code, w.stderr.String())
	}
	// the declaration lets everything through
	allowed := func(reg *registry.Registry, c *fail.Collector, log *[]string) {
		NewRegistration("example.com/app/srv", func() *appSrv { return &appSrv{log: log, cfg: appCfg{Version: 1, Greeting: "default"}} },
			func(s *appSrv) *appCfg { return &s.cfg }).
			Alias("srv").AllowsSuperuser().registerInto(reg, c)
	}
	w, code = appWorldWith(t, Builder().AcceptAll(), []string{"bin"}, nil, nil, allowed, asRoot)
	if code != 0 {
		t.Errorf("the declaration must let the applet run: code=%d\n%s", code, w.stderr.String())
	}
	// a plain id is unaffected
	w, code = appWorld(t, Builder().AcceptAll(), []string{"bin"}, nil, nil, register)
	if code != 0 {
		t.Errorf("non-root runs are untouched: code=%d\n%s", code, w.stderr.String())
	}
}

func TestBuildSurfacesCommitViolations(t *testing.T) {
	reg, catalogC := catalogWorld()
	NewBareRegistration("example.com/app/bad", func() *appAux { return &appAux{} }).
		registerInto(reg, catalogC) // no alias: a commit violation
	_, err := Builder().AcceptAll().buildFrom(reg, catalogC)
	if err == nil || !strings.Contains(err.Error(), "an alias is required") {
		t.Errorf("Build must surface commit violations: %v", err)
	}
}

func TestAmbiguityResolvedByOrderEndToEnd(t *testing.T) {
	register := func(reg *registry.Registry, c *fail.Collector, log *[]string) {
		NewBareRegistration("example.com/app/consumer", func() *catConsumer { return &catConsumer{log: log} }).
			Alias("consumer").registerInto(reg, c)
		NewBareRegistration("example.com/app/one", func() *bldA { return &bldA{} }).
			Alias("one").Provides(Iface[catIface]()).registerInto(reg, c)
		NewBareRegistration("example.com/app/two", func() *bldB { return &bldB{} }).
			Alias("two").Provides(Iface[catIface]()).registerInto(reg, c)
	}
	// unranked tie → startup violation pointing at the vet tool
	w, code := appWorld(t, Builder().AcceptAll(), []string{"bin"}, nil, nil, register)
	if code != 2 || !strings.Contains(w.stderr.String(), "sxcli-vet") {
		t.Errorf("unranked tie must refuse to start with the vet nudge: code=%d\n%s", code, w.stderr.String())
	}
	// Order resolves it — the ranked provider wins
	w, code = appWorld(t, Builder().AcceptAll().Order("example.com/app/two"), []string{"bin"}, nil, nil, register)
	if code != 0 || strings.Join(w.log, ",") != "consumer.run:*fw.bldB" {
		t.Errorf("rank must choose: code=%d log=%v stderr:\n%s", code, w.log, w.stderr.String())
	}
}

type catConsumer struct {
	Dep catIface `inject:""`
	log *[]string
}

func (a *catConsumer) Configured() error { return nil }
func (a *catConsumer) Run() int {
	*a.log = append(*a.log, fmt.Sprintf("consumer.run:%T", a.Dep))
	return 0
}

func TestOrderSequenceDecidesAmongRanked(t *testing.T) {
	register := func(reg *registry.Registry, c *fail.Collector, log *[]string) {
		NewBareRegistration("example.com/app/consumer", func() *catConsumer { return &catConsumer{log: log} }).
			Alias("consumer").registerInto(reg, c)
		NewBareRegistration("example.com/app/one", func() *bldA { return &bldA{} }).
			Alias("one").Provides(Iface[catIface]()).registerInto(reg, c)
		NewBareRegistration("example.com/app/two", func() *bldB { return &bldB{} }).
			Alias("two").Provides(Iface[catIface]()).registerInto(reg, c)
		NewBareRegistration("example.com/app/three", func() *catService { return &catService{} }).
			Alias("three").Provides(Iface[catIface]()).registerInto(reg, c)
	}
	// two of three ranked: the Order-earlier one wins the field
	w, code := appWorld(t, Builder().AcceptAll().
		Order("example.com/app/two", "example.com/app/one"), []string{"bin"}, nil, nil, register)
	if code != 0 || strings.Join(w.log, ",") != "consumer.run:*fw.bldB" {
		t.Errorf("Order-earlier must win: code=%d log=%v stderr:\n%s", code, w.log, w.stderr.String())
	}
	// flipping the sequence flips the winner — the sequence IS the semantics
	w, code = appWorld(t, Builder().AcceptAll().
		Order("example.com/app/one", "example.com/app/two"), []string{"bin"}, nil, nil, register)
	if code != 0 || strings.Join(w.log, ",") != "consumer.run:*fw.bldA" {
		t.Errorf("flipped Order must flip the winner: code=%d log=%v", code, w.log)
	}
}

// appOutsider is registered but referenced by nobody: the scoping
// pin's control group.
type appOutsider struct{}

func (a *appOutsider) Configured() error { return nil }

// appProbe runs assertions against the injected System facade from
// inside the composed world — the resolved service set ejects like
// any other; the
// views answer from the attach-time snapshot.
type appProbe struct {
	Sys  system.System `inject:""`
	Desc *appAux       `inject:"example.com/app/described"`
	do   func(sys system.System)
}

func (p *appProbe) Configured() error { return nil }
func (p *appProbe) Run() int          { p.do(p.Sys); return 0 }

func TestIntrospectionSpeaksAliasesTargetScoped(t *testing.T) {
	var applets, services []string
	var single, descByAlias, descByID, descOutside string
	var args []ArgInfo
	var byID, unknown system.Introspector
	register := func(reg *registry.Registry, c *fail.Collector, log *[]string) {
		NewBareRegistration("example.com/app/probe", func() *appProbe {
			return &appProbe{do: func(sys system.System) {
				binary := sys.Introspector("")
				applets = binary.Applets()
				single, _ = binary.SingleApplet()
				view := sys.Introspector("probe")
				services = view.Services()
				descByAlias = view.Describe("described")
				descByID = view.Describe("example.com/app/described")
				descOutside = view.Describe("outsider")
				args = view.Arguments(nil)
				byID = sys.Introspector("example.com/app/probe")
				unknown = sys.Introspector("ghost")
			}}
		}).Alias("probe").registerInto(reg, c)
		NewBareRegistration("example.com/app/described", func() *appAux { return &appAux{log: log} }).
			Alias("described").Metadata(&Metadata{Description: "a well-described service"}).
			registerInto(reg, c)
		NewBareRegistration("example.com/app/outsider", func() *appOutsider { return &appOutsider{} }).
			Alias("outsider").Metadata(&Metadata{Description: "not in the resolved service set"}).
			registerInto(reg, c)
	}
	w, code := appWorld(t, Builder().AcceptAll(), []string{"bin"}, nil, nil, register)
	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
	if strings.Join(applets, ",") != "probe" {
		t.Errorf("Applets must speak aliases: %v", applets)
	}
	if single != "probe" {
		t.Errorf("SingleApplet must speak the alias: %q", single)
	}
	joined := strings.Join(services, ",")
	if services[0] != "core" || !strings.Contains(joined, "described") || !strings.Contains(joined, "system") || strings.Contains(joined, "example.com") {
		t.Errorf("Services must be operator names, core first, resolved service set members only: %v", services)
	}
	if strings.Contains(joined, "outsider") {
		t.Errorf("Services must not reach past the resolved graph: %v", services)
	}
	if descByAlias != "a well-described service" || descByID != descByAlias {
		t.Errorf("Describe must accept both vocabularies inside the graph: %q / %q", descByAlias, descByID)
	}
	if descOutside != "" {
		t.Errorf("Describe must not reach past the resolved graph: %q", descOutside)
	}
	if len(args) == 0 {
		t.Error("the target view must carry the schema true to the resolved service set")
	}
	if byID != nil {
		t.Error("Introspector takes dispatch names, never ids")
	}
	if unknown != nil {
		t.Error("an unknown name is nil — offer nothing")
	}
}
