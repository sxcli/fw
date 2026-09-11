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
	"io"
	"strings"
	"sxcli.dev/conf/fail"
	"sxcli.dev/fw/internal/registry"
	"sxcli.dev/fw/system"
	"testing"
)

// introApplet records what the injected System facade reports during
// Run — after ejection, from the attach-time snapshot.
type introApplet struct {
	Sys      system.System `inject:""`
	applets  []string
	services []string
	exts     []string
}

func (a *introApplet) Configured() error { return nil }
func (a *introApplet) Run() int {
	binary := a.Sys.Introspector("")
	a.applets = binary.Applets()
	a.exts = binary.ConfigExtensions()
	if view := a.Sys.Introspector("meta"); view != nil {
		a.services = view.Services()
	}
	return 0
}

// fakeProvider claims a fantasy extension for ConfigExtensions tests.
type fakeProvider struct{}

func (p *fakeProvider) Extensions() []string                     { return []string{"toml", "json5"} }
func (p *fakeProvider) ToJSON(in io.Reader) (io.Reader, error)   { return in, nil }
func (p *fakeProvider) FromJSON(in io.Reader) (io.Reader, error) { return in, nil }

// argsProbe is an applet whose Run executes test-provided behavior
// against the injected System facade.
type argsProbe struct {
	Sys system.System `inject:""`
	do  func(sys system.System)
}

func (p *argsProbe) Configured() error { return nil }
func (p *argsProbe) Run() int {
	p.do(p.Sys)
	return 0
}

// extraService is cold unless enabled; its flag proves argument
// introspection is true to the resolved service set.
type extraCfg struct {
	Version uint32   `json:"version"`
	Flag    string   `json:"flag" conf:"extra-flag" usage:"only visible when extra is enabled"`
	Tags    []string `json:"tags" conf:"extra-tag" usage:"repeatable, domain-checkable"`
}

type extraService struct {
	cfg extraCfg
}

func longs(infos []ArgInfo) string {
	var out []string
	for _, a := range infos {
		if a.Long != "" {
			out = append(out, a.Long)
		}
	}
	return "," + strings.Join(out, ",") + ","
}

func argsWorld(t *testing.T, files map[string]string, do func(sys system.System)) *world {
	t.Helper()
	w := newWorld(t, []string{"bin", "meta"}, files, nil)
	w.applet(0) // "app", with its optional dep field
	probe := &argsProbe{do: do}
	NewBareRegistration("test/meta", func() *argsProbe { return probe }).
		Alias("meta").registerInto(w.cat, w.c)
	extra := &extraService{cfg: extraCfg{Version: 1, Flag: "default"}}
	NewRegistration("test/extra", func() *extraService { return extra },
		func(x *extraService) *extraCfg { return &x.cfg }).
		Alias("extra").registerInto(w.cat, w.c)
	return w
}

func TestArgumentsReportsResolvedServiceSetSchema(t *testing.T) {
	var infos []ArgInfo
	w := argsWorld(t, nil, func(sys system.System) {
		infos = sys.Introspector("app").Arguments(nil)
	})
	if code := w.run(); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
	all := longs(infos)
	if !strings.Contains(all, ",greeting,") || !strings.Contains(all, ",config,") {
		t.Errorf("schema must contain the applet's and the core's arguments: %v", all)
	}
	if strings.Contains(all, ",extra-flag,") {
		t.Errorf("cold service's arguments must be absent: %v", all)
	}
}

func TestIntrospectionIgnoresConfigFiles(t *testing.T) {
	// THE determinism pin (target-scoped-introspection design): the
	// view is built from the catalog alone — a config file enabling a
	// service must not change the answer, whether named in-line or
	// found by any search. This is the regression test for the
	// env→resolved-service-set vector the 2026-07-29 review proved.
	files := map[string]string{"/inline/cfg.json": `{"core": {"enable": ["extra"]}}`}
	var withC, withoutC []ArgInfo
	w := argsWorld(t, files, func(sys system.System) {
		withC = sys.Introspector("app").Arguments([]string{"-c", "/inline/cfg.json"})
		withoutC = sys.Introspector("app").Arguments(nil)
	})
	if code := w.run(); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
	if strings.Contains(longs(withC), ",extra-flag,") {
		t.Errorf("a config file must not shape introspection: %v", longs(withC))
	}
	if longs(withC) != longs(withoutC) {
		t.Errorf("same binary, same target, same answer — always:\n%v\n%v", longs(withC), longs(withoutC))
	}
}

func TestArgumentsArgsAreInert(t *testing.T) {
	// args are reserved for the explicit-control-vocabulary era; today
	// controls, transforms and poison alike are inert data
	var plain, controlled, poisoned []ArgInfo
	w := argsWorld(t, nil, func(sys system.System) {
		view := sys.Introspector("app")
		plain = view.Arguments(nil)
		controlled = view.Arguments([]string{"--disable", "ghost"})
		poisoned = view.Arguments([]string{"--upgrade-config", "--config", "/nowhere.json"})
	})
	if code := w.run(); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
	if len(plain) == 0 || longs(plain) != longs(controlled) || longs(plain) != longs(poisoned) {
		t.Errorf("inert args must not change the answer:\n%v\n%v\n%v", longs(plain), longs(controlled), longs(poisoned))
	}
}

func TestIntrospectionIsSideEffectFree(t *testing.T) {
	files := map[string]string{"/inline/cfg.json": `{"core": {"enable": ["extra"]}, "extra": {"flag": "changed"}}`}
	w := newWorld(t, []string{"bin", "meta"}, files, nil)
	w.applet(0)
	extra := &extraService{cfg: extraCfg{Version: 1, Flag: "default"}}
	NewRegistration("test/extra", func() *extraService { return extra },
		func(x *extraService) *extraCfg { return &x.cfg }).
		Alias("extra").registerInto(w.cat, w.c)
	probe := &argsProbe{do: func(sys system.System) {
		sys.Introspector("app").Arguments([]string{"-c", "/inline/cfg.json", "--write-config"})
	}}
	NewBareRegistration("test/meta", func() *argsProbe { return probe }).
		Alias("meta").registerInto(w.cat, w.c)
	if code := w.run(); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
	if extra.cfg.Flag != "default" {
		t.Errorf("introspection must never fill live config structs: %q", extra.cfg.Flag)
	}
	if w.stdout.Len() != 0 {
		t.Errorf("--write-config in introspected args must be inert:\n%s", w.stdout.String())
	}
}

func TestIntrospectorRejectsNonApplets(t *testing.T) {
	var forService, forUnknown, forID system.Introspector
	w := argsWorld(t, nil, func(sys system.System) {
		forService = sys.Introspector("extra")
		forUnknown = sys.Introspector("nope")
		forID = sys.Introspector("test/meta")
	})
	if code := w.run(); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
	if forService != nil {
		t.Error("a plain service is not a target: nil")
	}
	if forUnknown != nil {
		t.Error("an unknown name is nil — offer nothing")
	}
	if forID != nil {
		t.Error("Introspector takes dispatch names, never ids")
	}
}

func TestIntrospectorAnswersFromSnapshotAfterEjection(t *testing.T) {
	w := newWorld(t, []string{"bin"}, nil, nil)
	a := &introApplet{}
	NewBareRegistration("test/meta", func() *introApplet { return a }).
		Alias("meta").registerInto(w.cat, w.c)
	w.dep(false) // cold: nothing references it
	NewBareRegistration("test/fakefmt", func() *fakeProvider { return &fakeProvider{} }).
		Alias("fakefmt").Provides(Iface[ConfigFormatProvider]()).registerInto(w.cat, w.c)
	if code := w.run(); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
	if strings.Join(a.applets, ",") != "meta" {
		t.Errorf("applets wrong: %v", a.applets)
	}
	// ejection is UNIFORM now — the cold provider left the WORKING
	// set even though the system service is in the resolved service
	// set; the catalog itself stays whole
	if _, stillThere := w.rt.ws.ByID("test/fakefmt"); stillThere {
		t.Error("ejection must be uniform; the snapshot answers, not the live registry")
	}
	if _, inCatalog := w.rt.reg.ByID("test/fakefmt"); !inCatalog {
		t.Error("the catalog is immutable — ejection shrinks only the working set")
	}
	// ...and the binary-level facts still answer from the snapshot
	if strings.Join(a.exts, ",") != "json,toml,json5" {
		t.Errorf("extensions must answer from the snapshot: %v", a.exts)
	}
	// the target view is scoped to the resolved service set: meta and
	// its system dep, the
	// core leading; the cold dep and the provider are NOT members
	joined := strings.Join(a.services, ",")
	if a.services[0] != "core" || !strings.Contains(joined, "meta") || !strings.Contains(joined, "system") {
		t.Errorf("services must be the resolved service set, core first: %v", a.services)
	}
	if strings.Contains(joined, "dep") || strings.Contains(joined, "fakefmt") {
		t.Errorf("services must not reach past the resolved graph: %v", a.services)
	}
}

func TestEjectionStillHappensWithoutIntrospector(t *testing.T) {
	w := newWorld(t, []string{"bin"}, nil, nil)
	w.applet(0)
	// genuinely unreferenced: nothing injects ConfigFormatProvider and
	// no config file matches its extensions
	NewBareRegistration("test/fakefmt", func() *fakeProvider { return &fakeProvider{} }).
		Alias("fakefmt").Provides(Iface[ConfigFormatProvider]()).registerInto(w.cat, w.c)
	if code := w.run(); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
	if _, stillThere := w.rt.ws.ByID("test/fakefmt"); stillThere {
		t.Error("cold services must be ejected from the working set")
	}
}

func TestSystemIdentityIsGuarded(t *testing.T) {
	// the system service owns its identity like any member — the
	// ordinary checks guard it. In a real binary fw's init registers
	// FIRST (imported packages init before their importer), so a
	// user claiming system.ID is a duplicate-id violation:
	c := &fail.Collector{}
	cat := registry.New(c)
	NewBareRegistration(system.ID, func() *systemService { return &systemService{} }).
		Alias(SystemAlias).
		Provides(Iface[system.System]()).
		core().
		registerInto(cat, c)
	NewBareRegistration(system.ID, func() *secondApplet { return &secondApplet{} }).
		Alias("intruder").registerInto(cat, c)
	if c.Len() == 0 {
		t.Fatal("claiming the system id must be a duplicate-id violation")
	}
	// and the system ALIAS is reserved against user registrations:
	c2 := &fail.Collector{}
	cat2 := registry.New(c2)
	NewBareRegistration("test/pretender", func() *secondApplet { return &secondApplet{} }).
		Alias(SystemAlias).registerInto(cat2, c2)
	if c2.Len() == 0 {
		t.Fatal("the system alias must be reserved against user services")
	}
}

func TestIntrospectionNeverReadsEnvironment(t *testing.T) {
	// the doc's pin: the per-keystroke view consults NO environment —
	// not "nothing sensitive", literally never called
	calls := 0
	var got []ArgInfo
	w := argsWorld(t, nil, func(sys system.System) {
		got = sys.Introspector("app").Arguments(nil)
	})
	inner := w.rt.lookupEnv
	w.rt.lookupEnv = func(name string) (string, bool) {
		calls++
		if inner != nil {
			return inner(name)
		}
		return "", false
	}
	if code := w.run(); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
	if len(got) == 0 {
		t.Fatal("the schema must still answer")
	}
	if calls != 0 {
		t.Errorf("the introspection view read the environment %d times — it must never", calls)
	}
}
