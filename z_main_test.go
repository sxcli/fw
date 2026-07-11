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
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"strings"
	"testing"

	"sxcli.dev/fw/internal/config"
	"sxcli.dev/fw/internal/fail"
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
	Greeting string `json:"greeting" arg:"greeting,g" usage:"the greeting"`
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
	rt     *runtime
	c      *fail.Collector
	log    []string
	stdout bytes.Buffer
	stderr bytes.Buffer
}

func newWorld(t *testing.T, argv []string, files map[string]string, env map[string]string) *world {
	t.Helper()
	w := &world{c: &fail.Collector{}}
	reg := registry.New(w.c, checkReservedID, checkAppletLifecycle, config.ValidateConfig)
	w.rt = &runtime{
		reg:  reg,
		c:    w.c,
		argv: argv,
		lookupEnv: func(name string) (string, bool) {
			v, ok := env[name]
			return v, ok
		},
		stdout: &w.stdout,
		stderr: &w.stderr,
		locations: func(appletID string) []config.Location {
			return []config.Location{{Base: "/etc/" + appletID + "/config"}}
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
	t.Cleanup(func() { positionals = nil })
	return w
}

func (w *world) applet(code int, opts ...RegisterOption) *mainApplet {
	a := &mainApplet{log: &w.log, code: code}
	all := append([]RegisterOption{WithConfig(&a.cfg)}, opts...)
	w.rt.reg.Register("app", a, foldOptions(all))
	return a
}

func (w *world) dep(failStart bool) *depService {
	d := &depService{log: &w.log, failStart: failStart}
	w.rt.reg.Register("dep", d, foldOptions([]RegisterOption{Provides[depIface]()}))
	return d
}

func foldOptions(opts []RegisterOption) registry.Options {
	var o registerOptions
	for _, opt := range opts {
		opt(&o)
	}
	return registry.Options{Interfaces: o.interfaces, Config: o.config}
}

// ---- tests -------------------------------------------------------------

func TestHappyPathLifecycleOrder(t *testing.T) {
	w := newWorld(t, []string{"bin"}, nil, nil)
	a := w.applet(7)
	a.D = nil
	w.dep(false)
	// make the applet require the dep so it joins the closure
	w.rt.reg.All()[0].Deps[0].Optional = false
	code := run(w.rt)
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
	if code := run(w.rt); code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, w.stderr.String())
	}
	if a.cfg.Greeting != "hi" {
		t.Errorf("arg not applied: %q", a.cfg.Greeting)
	}
	if strings.Join(Positionals(), ",") != "one,two" {
		t.Errorf("positionals wrong: %v", Positionals())
	}
}

func TestPrecedenceFileEnvArg(t *testing.T) {
	files := map[string]string{"/etc/app/config.json": `{"app": {"greeting": "file"}}`}
	env := map[string]string{"APP_GREETING": "env"}
	w := newWorld(t, []string{"bin"}, files, env)
	a := w.applet(0)
	if code := run(w.rt); code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, w.stderr.String())
	}
	if a.cfg.Greeting != "env" {
		t.Errorf("env must beat file: %q", a.cfg.Greeting)
	}
	w2 := newWorld(t, []string{"bin", "-g", "arg"}, files, env)
	a2 := w2.applet(0)
	run(w2.rt)
	if a2.cfg.Greeting != "arg" {
		t.Errorf("arg must beat env: %q", a2.cfg.Greeting)
	}
}

func TestMultiAppletDispatch(t *testing.T) {
	w := newWorld(t, []string{"bin", "second"}, nil, nil)
	w.applet(0)
	w.rt.reg.Register("second", &secondApplet{log: &w.log}, registry.Options{})
	if code := run(w.rt); code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, w.stderr.String())
	}
	if strings.Join(w.log, ",") != "second.run" {
		t.Errorf("selector dispatch wrong: %v", w.log)
	}
}

func TestDispatchByBinaryName(t *testing.T) {
	w := newWorld(t, []string{"/usr/bin/second"}, nil, nil)
	w.applet(0)
	w.rt.reg.Register("second", &secondApplet{log: &w.log}, registry.Options{})
	if code := run(w.rt); code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, w.stderr.String())
	}
	if strings.Join(w.log, ",") != "second.run" {
		t.Errorf("argv0 dispatch wrong: %v", w.log)
	}
}

func TestDispatchFailuresPrintUsage(t *testing.T) {
	w := newWorld(t, []string{"bin", "ghost"}, nil, nil)
	w.applet(0)
	w.rt.reg.Register("second", &secondApplet{log: &w.log}, registry.Options{})
	if code := run(w.rt); code != 2 {
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
	w.rt.reg.Register("app", &secondApplet{log: &w.log}, registry.Options{}) // duplicate id
	if code := run(w.rt); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(w.stderr.String(), "duplicate id") {
		t.Errorf("violation not reported:\n%s", w.stderr.String())
	}
}

func TestUnknownArgumentFails(t *testing.T) {
	w := newWorld(t, []string{"bin", "--nope"}, nil, nil)
	w.applet(0)
	if code := run(w.rt); code != 2 {
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
	if code := run(w.rt); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

func TestConfiguredFailureAbortsBeforeStart(t *testing.T) {
	w := newWorld(t, []string{"bin"}, nil, nil)
	a := w.applet(0)
	a.fail = true
	if code := run(w.rt); code != 2 {
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
	w.rt.reg.Register("failer", failer, foldOptions([]RegisterOption{Provides[depIface]()}))
	// applet → failer (by id) → dep: dep starts first, failer's Start
	// breaks, dep must be stopped
	w.rt.reg.All()[0].Deps[0].Optional = false
	w.rt.reg.All()[0].Deps[0].IDs = []string{"failer"}
	code := run(w.rt)
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
	w.rt.reg.All()[0].Deps[0].IDs = []string{"dep"}
	if code := run(w.rt); code != 0 {
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
	if code := run(w.rt); code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, w.stderr.String())
	}
	out := w.stdout.String()
	if !strings.Contains(out, `"greeting": "dumped"`) || !strings.Contains(out, `"core"`) {
		t.Errorf("dump wrong:\n%s", out)
	}
	if strings.Contains(strings.Join(w.log, ","), "applet.run") {
		t.Errorf("write-config must not run the applet: %v", w.log)
	}
}

func TestHelpRendersSchema(t *testing.T) {
	w := newWorld(t, []string{"bin", "--help"}, nil, nil)
	w.applet(0)
	if code := run(w.rt); code != 0 {
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
	if code := run(w.rt); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(w.stderr.String(), "greeting=logged") {
		t.Errorf("applet log must reach the fallback stderr handler:\n%s", w.stderr.String())
	}
}
