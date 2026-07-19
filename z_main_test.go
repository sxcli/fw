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
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"strings"
	"testing"

	"sxcli.dev/conf/engine"
	"sxcli.dev/conf/fail"
	"sxcli.dev/fw/internal/registry"
)

// ---- fixtures ----------------------------------------------------------

type depIface interface{ Dep() }

type depService struct {
	log       *[]string
	failStart bool
}

func (d *depService) Dep() {}
func (d *depService) Configured() error {
	*d.log = append(*d.log, "dep.configured")
	return nil
}
func (d *depService) Start() error {
	var err error
	if d.failStart {
		err = errors.New("dep start broke")
	} else {
		*d.log = append(*d.log, "dep.start")
	}
	return err
}
func (d *depService) Stop() error {
	*d.log = append(*d.log, "dep.stop")
	return nil
}

type mainAppletCfg struct {
	Version  uint32   `json:"version"`
	Greeting string   `json:"greeting" conf:"greeting,g" usage:"the greeting"`
	Rest     []string `json:"rest" pos:"rest" usage:"trailing invocation data"`
}

type mainApplet struct {
	log  *[]string
	code int
	cfg  mainAppletCfg
	D    depIface `inject:";optional"`
	fail bool
}

func (a *mainApplet) Configured() error {
	var err error
	if a.fail {
		err = errors.New("applet config broke")
	} else {
		*a.log = append(*a.log, "applet.configured")
	}
	return err
}

func (a *mainApplet) Run() int {
	*a.log = append(*a.log, "applet.run")
	slog.Info("applet says", "greeting", a.cfg.Greeting)
	return a.code
}

// failerService needs dep and breaks on Start.
type failerService struct {
	log *[]string
	D   depIface `inject:"dep"`
}

func (f *failerService) Dep()              {}
func (f *failerService) Configured() error { return nil }
func (f *failerService) Start() error      { return errors.New("failer start broke") }
func (f *failerService) Stop() error {
	*f.log = append(*f.log, "failer.stop")
	return nil
}

type secondApplet struct {
	log *[]string
}

func (a *secondApplet) Configured() error { return nil }
func (a *secondApplet) Run() int {
	*a.log = append(*a.log, "second.run")
	return 0
}

// ---- harness -----------------------------------------------------------

type world struct {
	cat    *registry.Registry // the private catalog; chains registerInto it
	rt     *runtime
	c      *fail.Collector
	log    []string
	stdout bytes.Buffer
	stderr bytes.Buffer
}

func newWorld(t *testing.T, argv []string, files map[string]string, env map[string]string) *world {
	t.Helper()
	w := &world{c: &fail.Collector{}}
	w.cat = registry.New(w.c)
	w.rt = &runtime{
		c:    w.c,
		argv: argv,
		lookupEnv: func(name string) (string, bool) {
			v, ok := env[name]
			return v, ok
		},
		stdout: &w.stdout,
		stderr: &w.stderr,
		locations: func(appletID string) []engine.Location {
			return []engine.Location{{Base: "/etc/" + appletID + "/config"}}
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
	return w
}

// build composes the catalog (AcceptAll — worlds are busybox-shaped)
// without running; tests inspecting composed state use it directly.
func (w *world) build() error {
	app, err := Builder().AcceptAll().buildFrom(w.cat, w.c)
	if err == nil {
		w.rt.reg = app.reg
	}
	return err
}

// run composes and drives the pipeline. A Build failure reports like
// production: all violations to stderr, exit 2.
func (w *world) run() int {
	code := 2
	if err := w.build(); err != nil {
		fmt.Fprintf(&w.stderr, "error: %v\n", err)
	} else {
		code = run(w.rt)
	}
	return code
}

// applet registers the world's main applet. World factories return
// the pre-built instance so tests can hold it across the run — a
// deliberate deviation from the fresh-per-Make contract that worlds,
// building exactly once, are entitled to.
func (w *world) applet(code int) *mainApplet {
	a := &mainApplet{log: &w.log, code: code, cfg: mainAppletCfg{Version: 1}}
	NewRegistration("app", func() *mainApplet { return a },
		func(x *mainApplet) *mainAppletCfg { return &x.cfg }).
		Alias("app").registerInto(w.cat, w.c)
	return a
}

func (w *world) dep(failStart bool) *depService {
	d := &depService{log: &w.log, failStart: failStart}
	NewBareRegistration("dep", func() *depService { return d }).
		Alias("dep").Provides(Iface[depIface]()).registerInto(w.cat, w.c)
	return d
}

// ---- tests -------------------------------------------------------------

func TestHappyPathLifecycleOrder(t *testing.T) {
	w := newWorld(t, []string{"bin"}, nil, nil)
	a := w.applet(7)
	a.D = nil
	w.dep(false)
	// make the applet require the dep so it joins the closure
	w.cat.All()[0].Deps[0].Optional = false
	code := w.run()
	if code != 7 {
		t.Fatalf("exit code = %d, want 7; stderr:\n%s", code, w.stderr.String())
	}
	want := []string{"dep.configured", "applet.configured", "dep.start", "applet.run", "dep.stop"}
	if strings.Join(w.log, ",") != strings.Join(want, ",") {
		t.Errorf("lifecycle order wrong: %v", w.log)
	}
}

func TestSingleAppletModeCollectsPositionals(t *testing.T) {
	w := newWorld(t, []string{"bin", "--greeting=hi", "one", "two"}, nil, nil)
	a := w.applet(0)
	if code := w.run(); code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, w.stderr.String())
	}
	if a.cfg.Greeting != "hi" {
		t.Errorf("arg not applied: %q", a.cfg.Greeting)
	}
	// Ledger note: Positionals() died with the declared-positional
	// pass — the applet reads its own pos:"rest" field now
	if strings.Join(a.cfg.Rest, ",") != "one,two" {
		t.Errorf("positionals wrong: %v", a.cfg.Rest)
	}
}

func TestPrecedenceFileEnvArg(t *testing.T) {
	files := map[string]string{"/etc/app/config.json": `{"app": {"greeting": "file"}}`}
	env := map[string]string{"APP__GREETING": "env"}
	w := newWorld(t, []string{"bin"}, files, env)
	a := w.applet(0)
	if code := w.run(); code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, w.stderr.String())
	}
	if a.cfg.Greeting != "env" {
		t.Errorf("env must beat file: %q", a.cfg.Greeting)
	}
	w2 := newWorld(t, []string{"bin", "-g", "arg"}, files, env)
	a2 := w2.applet(0)
	w2.run()
	if a2.cfg.Greeting != "arg" {
		t.Errorf("arg must beat env: %q", a2.cfg.Greeting)
	}
}

func TestMultiAppletDispatch(t *testing.T) {
	w := newWorld(t, []string{"bin", "second"}, nil, nil)
	w.applet(0)
	NewBareRegistration("second", func() *secondApplet { return &secondApplet{log: &w.log} }).
		Alias("second").registerInto(w.cat, w.c)
	if code := w.run(); code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, w.stderr.String())
	}
	if strings.Join(w.log, ",") != "second.run" {
		t.Errorf("selector dispatch wrong: %v", w.log)
	}
}

func TestDispatchByBinaryName(t *testing.T) {
	w := newWorld(t, []string{"/usr/bin/second"}, nil, nil)
	w.applet(0)
	NewBareRegistration("second", func() *secondApplet { return &secondApplet{log: &w.log} }).
		Alias("second").registerInto(w.cat, w.c)
	if code := w.run(); code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, w.stderr.String())
	}
	if strings.Join(w.log, ",") != "second.run" {
		t.Errorf("argv0 dispatch wrong: %v", w.log)
	}
}

func TestDispatchFailuresPrintUsage(t *testing.T) {
	w := newWorld(t, []string{"bin", "ghost"}, nil, nil)
	w.applet(0)
	NewBareRegistration("second", func() *secondApplet { return &secondApplet{log: &w.log} }).
		Alias("second").registerInto(w.cat, w.c)
	if code := w.run(); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	text := w.stderr.String()
	if !strings.Contains(text, "usage:") || !strings.Contains(text, "app") || !strings.Contains(text, "second") {
		t.Errorf("usage dump wrong:\n%s", text)
	}
}

func TestRegistrationErrorsAbort(t *testing.T) {
	w := newWorld(t, []string{"bin"}, nil, nil)
	w.applet(0)
	NewBareRegistration("app", func() *secondApplet { return &secondApplet{log: &w.log} }).
		Alias("app").registerInto(w.cat, w.c) // duplicate id
	if code := w.run(); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(w.stderr.String(), "duplicate id") {
		t.Errorf("violation not reported:\n%s", w.stderr.String())
	}
}

func TestUnknownArgumentFails(t *testing.T) {
	w := newWorld(t, []string{"bin", "--nope"}, nil, nil)
	w.applet(0)
	if code := w.run(); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(w.stderr.String(), "unknown argument --nope") {
		t.Errorf("strict parse error missing:\n%s", w.stderr.String())
	}
}

func TestSuppressedFeatureIsUnknown(t *testing.T) {
	w := newWorld(t, []string{"bin", "--config", "x.json"}, nil, nil)
	w.applet(0)
	w.rt.suppressed = []string{"config"}
	if code := w.run(); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

func TestConfiguredFailureAbortsBeforeStart(t *testing.T) {
	w := newWorld(t, []string{"bin"}, nil, nil)
	a := w.applet(0)
	a.fail = true
	if code := w.run(); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if strings.Contains(strings.Join(w.log, ","), "applet.run") {
		t.Errorf("applet must not run after a Configured failure: %v", w.log)
	}
	if !strings.Contains(w.stderr.String(), "applet config broke") {
		t.Errorf("error not reported:\n%s", w.stderr.String())
	}
}

func TestStartFailureStopsStarted(t *testing.T) {
	w := newWorld(t, []string{"bin"}, nil, nil)
	w.applet(0)
	w.dep(false)
	failer := &failerService{log: &w.log}
	NewBareRegistration("failer", func() *failerService { return failer }).
		Alias("failer").Provides(Iface[depIface]()).registerInto(w.cat, w.c)
	// applet → failer (by id) → dep: dep starts first, failer's Start
	// breaks, dep must be stopped
	w.cat.All()[0].Deps[0].Optional = false
	w.cat.All()[0].Deps[0].IDs = []string{"failer"}
	code := w.run()
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr:\n%s", code, w.stderr.String())
	}
	joined := strings.Join(w.log, ",")
	if strings.Contains(joined, "applet.run") {
		t.Errorf("applet must not run after a Start failure: %v", w.log)
	}
	if !strings.Contains(joined, "dep.start") || !strings.Contains(joined, "dep.stop") {
		t.Errorf("started service must be stopped: %v", w.log)
	}
}

func TestDisableStripsOptionalDependency(t *testing.T) {
	w := newWorld(t, []string{"bin", "--disable", "dep"}, nil, nil)
	a := w.applet(0)
	w.dep(false)
	w.cat.All()[0].Deps[0].IDs = []string{"dep"}
	if code := w.run(); code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, w.stderr.String())
	}
	if a.D != nil {
		t.Error("disabled optional dependency must stay nil")
	}
	if strings.Contains(strings.Join(w.log, ","), "dep.configured") {
		t.Errorf("disabled service must stay cold: %v", w.log)
	}
}

func TestWriteConfigToStdout(t *testing.T) {
	w := newWorld(t, []string{"bin", "--write-config", "--greeting", "dumped"}, nil, nil)
	w.applet(0)
	if code := w.run(); code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, w.stderr.String())
	}
	out := w.stdout.String()
	if !strings.Contains(out, `"greeting": "dumped"`) {
		t.Errorf("dump wrong:\n%s", out)
	}
	if strings.Contains(out, `"core"`) {
		t.Errorf("empty core section must be omitted from the dump:\n%s", out)
	}
	if strings.Contains(strings.Join(w.log, ","), "applet.run") {
		t.Errorf("write-config must not run the applet: %v", w.log)
	}
}

func TestHelpRendersSchema(t *testing.T) {
	w := newWorld(t, []string{"bin", "--help"}, nil, nil)
	w.applet(0)
	if code := w.run(); code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, w.stderr.String())
	}
	out := w.stdout.String()
	if !strings.Contains(out, "--greeting, -g") || !strings.Contains(out, "the greeting") || !strings.Contains(out, "--config, -c") {
		t.Errorf("help output wrong:\n%s", out)
	}
	if strings.Contains(strings.Join(w.log, ","), "applet.run") {
		t.Errorf("help must not run the applet: %v", w.log)
	}
}

func TestStartupLogsReachFallbackStderr(t *testing.T) {
	w := newWorld(t, []string{"bin", "--greeting", "logged"}, nil, nil)
	w.applet(0)
	if code := w.run(); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(w.stderr.String(), "greeting=logged") {
		t.Errorf("applet log must reach the fallback stderr handler:\n%s", w.stderr.String())
	}
}
