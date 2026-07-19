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

// The framework side of the front-door parity passes: best-effort
// --help, --validate-config, the registration chain's Migrate, and
// --upgrade-config over the whole catalog.
package fw

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sxcli.dev/fw/conf/engine"
)

func TestHelpBestEffortOverBrokenConfig(t *testing.T) {
	files := map[string]string{"/etc/app/config.json": `{"app": {"nope": true}}`}
	w := newWorld(t, []string{"bin", "--help"}, files, nil)
	w.applet(0)
	if code := w.run(); code != 0 {
		t.Fatalf("help must be served despite violations: exit %d", code)
	}
	if !strings.Contains(w.stdout.String(), "--greeting") {
		t.Errorf("help must render the schema:\n%s", w.stdout.String())
	}
	if !strings.Contains(w.stderr.String(), "unknown key") {
		t.Errorf("the violations must not be swallowed:\n%s", w.stderr.String())
	}
}

func TestHelpFallsBackWithoutSchema(t *testing.T) {
	// unparseable file: the plan dies before a schema exists, so help
	// answers from the registration-level fallback
	files := map[string]string{"/etc/app/config.json": `{{{not json`}
	w := newWorld(t, []string{"bin", "--help"}, files, nil)
	w.applet(0)
	if code := w.run(); code != 0 {
		t.Fatalf("help must fall back, not fail: exit %d\n%s", code, w.stderr.String())
	}
	if !strings.Contains(w.stdout.String(), "--greeting") {
		t.Errorf("fallback help must render the registration-level schema:\n%s", w.stdout.String())
	}
	if w.stderr.Len() == 0 {
		t.Error("the parse violation must still be reported")
	}
}

func TestHelpMarksSuspectValues(t *testing.T) {
	// the greeting arrives broken from the file: its value column must
	// say "error", not show a half-truth
	files := map[string]string{"/etc/app/config.json": `{"app": {"greeting": 42}}`}
	w := newWorld(t, []string{"bin", "--help"}, files, nil)
	w.applet(0)
	if code := w.run(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.stdout.String(), "value: error") {
		t.Errorf("a source-errored field must show 'error':\n%s", w.stdout.String())
	}
}

func TestValidateConfig(t *testing.T) {
	w := newWorld(t, []string{"bin", "--validate-config"}, nil, nil)
	w.applet(0)
	if code := w.run(); code != 0 {
		t.Fatalf("a clean config must validate silently: exit %d\n%s", code, w.stderr.String())
	}
	if strings.Contains(strings.Join(w.log, ","), "applet.run") {
		t.Errorf("validate must never run the applet: %v", w.log)
	}
	files := map[string]string{"/etc/app/config.json": `{"app": {"nope": true}}`}
	w2 := newWorld(t, []string{"bin", "--validate-config"}, files, nil)
	w2.applet(0)
	if code := w2.run(); code != 2 {
		t.Fatalf("a violated config must exit 2, got %d", code)
	}
	if !strings.Contains(w2.stderr.String(), "unknown key") {
		t.Errorf("the violations are the entire point:\n%s", w2.stderr.String())
	}
}

// The chain's Migrate: a v1 file section walks to the current schema
// before the merge, end to end through the framework pipeline.
type parityCfgV1 struct {
	Version  uint32 `json:"version"`
	Greeting string `json:"welcome"` // renamed to greeting in v2
}

func TestMigrateOnTheRegistrationChain(t *testing.T) {
	files := map[string]string{"/etc/app/config.json": `{"app": {"version": 1, "welcome": "old world"}}`}
	w := newWorld(t, []string{"bin"}, files, nil)
	a := &mainApplet{log: &w.log, cfg: mainAppletCfg{Version: 2}}
	NewRegistration("app", func() *mainApplet { return a },
		func(x *mainApplet) *mainAppletCfg { return &x.cfg }).
		Alias("app").
		Migrate(Step(1, func(old parityCfgV1) mainAppletCfg {
			return mainAppletCfg{Greeting: old.Greeting}
		})).registerInto(w.cat, w.c)
	if code := w.run(); code != 0 {
		t.Fatalf("exit %d\n%s", code, w.stderr.String())
	}
	if a.cfg.Greeting != "old world" || a.cfg.Version != 2 {
		t.Errorf("the chain must migrate the file section: %+v", a.cfg)
	}
}

func TestMigrateOnBareRegistrationIsViolation(t *testing.T) {
	w := newWorld(t, []string{"bin"}, nil, nil)
	w.applet(0)
	NewBareRegistration("bare", func() *plainService { return &plainService{} }).
		Alias("bare").
		Migrate(Step(1, func(old parityCfgV1) parityCfgV1 { return old })).
		registerInto(w.cat, w.c)
	if w.c.Len() == 0 {
		t.Fatal("Migrate without a config struct must be a commit violation")
	}
}

func TestUpgradeConfigCoversTheWholeCatalog(t *testing.T) {
	// the file holds a section of a service OUTSIDE the dispatched
	// applet's closure: the transform must still own it
	path := filepath.Join(t.TempDir(), "config.json")
	content := `{"app": {"version": 1, "welcome": "hi"}, "dep": {"version": 1}, "stranger": {"keep": true}}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	w := newWorld(t, []string{"bin", "--upgrade-config", "--config", path}, nil, nil)
	a := &mainApplet{log: &w.log, cfg: mainAppletCfg{Version: 2}}
	NewRegistration("app", func() *mainApplet { return a },
		func(x *mainApplet) *mainAppletCfg { return &x.cfg }).
		Alias("app").
		Migrate(Step(1, func(old parityCfgV1) mainAppletCfg {
			return mainAppletCfg{Greeting: old.Greeting}
		})).registerInto(w.cat, w.c)
	w.dep(false) // config-less: not part of the schema, so its section is foreign
	// the transform reads through the runtime's seams: real fs for this test
	w.rt.stat = engine.StatRegular
	w.rt.open = func(p string) (io.ReadCloser, error) { return os.Open(p) }
	if code := w.run(); code != 0 {
		t.Fatalf("exit %d\n%s", code, w.stderr.String())
	}
	raw, _ := os.ReadFile(path)
	out := string(raw)
	if !strings.Contains(out, `"greeting"`) || strings.Contains(out, `"welcome"`) {
		t.Errorf("the applet's section must be migrated: %s", out)
	}
	if !strings.Contains(out, `"stranger"`) || !strings.Contains(out, `"dep"`) {
		t.Errorf("foreign sections must pass through verbatim: %s", out)
	}
	if strings.Contains(strings.Join(w.log, ","), "applet.run") {
		t.Errorf("upgrade must never run the applet: %v", w.log)
	}
}

func TestUndeclaredTailIsViolation(t *testing.T) {
	// mainAppletCfg declares rest, so build a world around an applet
	// that declares nothing
	w := newWorld(t, []string{"bin", "second", "stray"}, nil, nil)
	w.applet(0)
	NewBareRegistration("second", func() *secondApplet { return &secondApplet{log: &w.log} }).
		Alias("second").registerInto(w.cat, w.c)
	if code := w.run(); code != 2 {
		t.Fatalf("an undeclared tail must be a violation: exit %d", code)
	}
	if !strings.Contains(w.stderr.String(), `unexpected positional "stray"`) {
		t.Errorf("the surplus token must be named:\n%s", w.stderr.String())
	}
}

func TestPositionalsAreAppletOnly(t *testing.T) {
	type posyCfg struct {
		Version uint32 `json:"version"`
		Thing   string `json:"thing" pos:"0"`
	}
	w := newWorld(t, []string{"bin"}, nil, nil)
	w.applet(0)
	NewRegistration("svc", func() *extraService { return &extraService{} },
		func(x *extraService) *extraCfg { return &x.cfg }).
		Alias("svc").registerInto(w.cat, w.c) // sanity: plain service commits
	before := w.c.Len()
	type posService struct{ cfg posyCfg }
	NewRegistration("posy", func() *posService { return &posService{cfg: posyCfg{Version: 1}} },
		func(x *posService) *posyCfg { return &x.cfg }).
		Alias("posy").registerInto(w.cat, w.c)
	if w.c.Len() == before {
		t.Fatal("pos fields on a non-applet service must be a commit violation")
	}
	joined := ""
	for _, err := range w.c.All() {
		joined += err.Error()
	}
	if !strings.Contains(joined, "applet") {
		t.Errorf("the violation must say why: %v", joined)
	}
}

func TestHelpRendersPositionalContract(t *testing.T) {
	w := newWorld(t, []string{"bin", "--help"}, nil, nil)
	w.applet(0) // mainAppletCfg declares pos:"rest"
	if code := w.run(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.stdout.String(), "<rest...>") {
		t.Errorf("help must render the positional contract:\n%s", w.stdout.String())
	}
}
