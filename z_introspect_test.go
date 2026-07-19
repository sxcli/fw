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
	"testing"
)

// introApplet records what the injected Introspector reports during Run.
type introApplet struct {
	I        *Introspector `inject:""`
	applets  []string
	services []string
	exts     []string
}

func (a *introApplet) Configured() error { return nil }
func (a *introApplet) Run() int {
	a.applets = a.I.Applets()
	a.services = a.I.Services()
	a.exts = a.I.ConfigExtensions()
	return 0
}

// fakeProvider claims a fantasy extension for ConfigExtensions tests.
type fakeProvider struct{}

func (p *fakeProvider) Extensions() []string                     { return []string{"toml", "json5"} }
func (p *fakeProvider) ToJSON(in io.Reader) (io.Reader, error)   { return in, nil }
func (p *fakeProvider) FromJSON(in io.Reader) (io.Reader, error) { return in, nil }

// argsProbe is an applet whose Run executes test-provided behavior
// against the injected Introspector.
type argsProbe struct {
	I  *Introspector `inject:""`
	do func(i *Introspector)
}

func (p *argsProbe) Configured() error { return nil }
func (p *argsProbe) Run() int {
	p.do(p.I)
	return 0
}

// extraService is cold unless enabled; its flag proves closure-true
// argument introspection.
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

func argsWorld(t *testing.T, files map[string]string, do func(i *Introspector)) *world {
	t.Helper()
	w := newWorld(t, []string{"bin", "meta"}, files, nil)
	w.applet(0) // "app", with its optional dep field
	probe := &argsProbe{do: do}
	NewBareRegistration("meta", func() *argsProbe { return probe }).
		Alias("meta").registerInto(w.cat, w.c)
	extra := &extraService{cfg: extraCfg{Version: 1, Flag: "default"}}
	NewRegistration("extra", func() *extraService { return extra },
		func(x *extraService) *extraCfg { return &x.cfg }).
		Alias("extra").registerInto(w.cat, w.c)
	return w
}

func TestArgumentsReportsClosureSchema(t *testing.T) {
	var infos []ArgInfo
	var err error
	w := argsWorld(t, nil, func(i *Introspector) {
		infos, err = i.Arguments("app", nil)
	})
	if code := w.run(); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	all := longs(infos)
	if !strings.Contains(all, ",greeting,") || !strings.Contains(all, ",config,") {
		t.Errorf("schema must contain the applet's and the core's arguments: %v", all)
	}
	if strings.Contains(all, ",extra-flag,") {
		t.Errorf("cold service's arguments must be absent: %v", all)
	}
}

func TestArgumentsHonorsInlineConfigAndControls(t *testing.T) {
	files := map[string]string{"/inline/cfg.json": `{"core": {"enable": ["extra"]}}`}
	var withC, withoutC []ArgInfo
	w := argsWorld(t, files, func(i *Introspector) {
		withC, _ = i.Arguments("app", []string{"-c", "/inline/cfg.json"})
		withoutC, _ = i.Arguments("app", nil)
	})
	if code := w.run(); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
	if !strings.Contains(longs(withC), ",extra-flag,") {
		t.Errorf("an in-line -c enabling a service must add its arguments: %v", longs(withC))
	}
	if strings.Contains(longs(withoutC), ",extra-flag,") {
		t.Errorf("without the -c the service stays cold: %v", longs(withoutC))
	}
}

func TestArgumentsBestEffortFallback(t *testing.T) {
	var infos []ArgInfo
	var err error
	w := argsWorld(t, nil, func(i *Introspector) {
		infos, err = i.Arguments("app", []string{"--disable", "ghost"})
	})
	if code := w.run(); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
	if err == nil {
		t.Error("a poisoned control must surface as an error")
	}
	if !strings.Contains(longs(infos), ",greeting,") {
		t.Errorf("fallback must still deliver the registration-level schema: %v", longs(infos))
	}
}

func TestArgumentsIsSideEffectFree(t *testing.T) {
	files := map[string]string{"/inline/cfg.json": `{"core": {"enable": ["extra"]}, "extra": {"flag": "changed"}}`}
	w := newWorld(t, []string{"bin", "meta"}, files, nil)
	w.applet(0)
	extra := &extraService{cfg: extraCfg{Version: 1, Flag: "default"}}
	NewRegistration("extra", func() *extraService { return extra },
		func(x *extraService) *extraCfg { return &x.cfg }).
		Alias("extra").registerInto(w.cat, w.c)
	probe := &argsProbe{do: func(i *Introspector) {
		i.Arguments("app", []string{"-c", "/inline/cfg.json", "--write-config"})
	}}
	NewBareRegistration("meta", func() *argsProbe { return probe }).
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

func TestArgumentsRejectsNonApplets(t *testing.T) {
	var errService, errUnknown error
	w := argsWorld(t, nil, func(i *Introspector) {
		_, errService = i.Arguments("extra", nil)
		_, errUnknown = i.Arguments("nope", nil)
	})
	if code := w.run(); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
	if errService == nil || !strings.Contains(errService.Error(), "not an applet") {
		t.Errorf("plain service must be rejected: %v", errService)
	}
	if errUnknown == nil || !strings.Contains(errUnknown.Error(), "not registered") {
		t.Errorf("unknown id must be rejected: %v", errUnknown)
	}
}

func TestIntrospectorReportsComposition(t *testing.T) {
	w := newWorld(t, []string{"bin"}, nil, nil)
	a := &introApplet{}
	NewBareRegistration("meta", func() *introApplet { return a }).
		Alias("meta").registerInto(w.cat, w.c)
	w.dep(false) // cold: nothing references it
	NewBareRegistration("fakefmt", func() *fakeProvider { return &fakeProvider{} }).
		Alias("fakefmt").Provides(Iface[ConfigFormatProvider]()).registerInto(w.cat, w.c)
	if code := w.run(); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
	if strings.Join(a.applets, ",") != "meta" {
		t.Errorf("applets wrong: %v", a.applets)
	}
	// ejection was skipped: the cold dep and the provider are still
	// enumerable, and the introspector lists itself
	joined := strings.Join(a.services, ",")
	for _, want := range []string{"meta", "dep", "fakefmt", "introspection"} {
		if !strings.Contains(joined, want) {
			t.Errorf("services must include %q (ejection skipped): %v", want, a.services)
		}
	}
	if strings.Join(a.exts, ",") != "json,toml,json5" {
		t.Errorf("extensions wrong: %v", a.exts)
	}
}

func TestEjectionStillHappensWithoutIntrospector(t *testing.T) {
	w := newWorld(t, []string{"bin"}, nil, nil)
	w.applet(0)
	// genuinely unreferenced: nothing injects ConfigFormatProvider and
	// no config file matches its extensions
	NewBareRegistration("fakefmt", func() *fakeProvider { return &fakeProvider{} }).
		Alias("fakefmt").Provides(Iface[ConfigFormatProvider]()).registerInto(w.cat, w.c)
	if code := w.run(); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
	if _, stillThere := w.rt.reg.ByID("fakefmt"); stillThere {
		t.Error("without the introspector in the closure, cold services must still be ejected")
	}
}

func TestIntrospectionIDIsReserved(t *testing.T) {
	w := newWorld(t, []string{"bin"}, nil, nil)
	w.applet(0)
	NewBareRegistration("introspection", func() *secondApplet { return &secondApplet{log: &w.log} }).
		Alias("introspection").registerInto(w.cat, w.c)
	if w.c.Len() == 0 {
		t.Fatal("foreign type under the introspection id must be a violation")
	}
	if code := w.run(); code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
}

func TestIntrospectorSquattingFailsLoudly(t *testing.T) {
	w := newWorld(t, []string{"bin"}, nil, nil)
	w.applet(0)
	// a squatter registers the core's concrete type under another id;
	// the core's own registration then collides on the concrete type
	NewBareRegistration("myintro", func() *Introspector { return &Introspector{} }).
		Alias("myintro").registerInto(w.cat, w.c)
	if code := w.run(); code != 2 {
		t.Errorf("exit = %d, want 2; squatting must fail startup", code)
	}
	// ledger note: the defense used to ride the old duplicate-type
	// registration check; it is an explicit reservation now
	if !strings.Contains(w.stderr.String(), "reserved for the core") {
		t.Errorf("expected the reserved-type violation:\n%s", w.stderr.String())
	}
}

func TestArgumentsTreatsUpgradeConfigAsInert(t *testing.T) {
	// an --upgrade-config plan carries no schema (the pure transform
	// never loads); a completion probe passing the token through must
	// get the registration-level answer, never a crash
	var infos []ArgInfo
	var err error
	w := argsWorld(t, nil, func(i *Introspector) {
		infos, err = i.Arguments("app", []string{"--upgrade-config", "--config", "/nowhere.json"})
	})
	if code := w.run(); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
	if err != nil {
		t.Fatalf("the token must be inert data, not a failure: %v", err)
	}
	all := longs(infos)
	if !strings.Contains(all, ",greeting,") || !strings.Contains(all, ",config,") {
		t.Errorf("the fallback must still answer registration-level: %v", all)
	}
}
