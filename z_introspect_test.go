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
	"io"
	"strings"
	"testing"

	"sxcli.dev/fw/internal/registry"
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

func TestIntrospectorReportsComposition(t *testing.T) {
	w := newWorld(t, []string{"bin"}, nil, nil)
	a := &introApplet{}
	w.rt.reg.Register("meta", a, foldOptions([]RegisterOption{}))
	w.dep(false) // cold: nothing references it
	w.rt.reg.Register("fakefmt", &fakeProvider{}, foldOptions([]RegisterOption{Provides[ConfigFormatProvider]()}))
	if code := run(w.rt); code != 0 {
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
	w.rt.reg.Register("fakefmt", &fakeProvider{}, foldOptions([]RegisterOption{Provides[ConfigFormatProvider]()}))
	if code := run(w.rt); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
	if _, stillThere := w.rt.reg.ByID("fakefmt"); stillThere {
		t.Error("without the introspector in the closure, cold services must still be ejected")
	}
}

func TestIntrospectionIDIsReserved(t *testing.T) {
	w := newWorld(t, []string{"bin"}, nil, nil)
	w.applet(0)
	w.rt.reg.Register("introspection", &secondApplet{log: &w.log}, registry.Options{})
	if w.c.Len() == 0 {
		t.Fatal("foreign type under the introspection id must be a violation")
	}
	if code := run(w.rt); code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
}

func TestIntrospectorSquattingFailsLoudly(t *testing.T) {
	w := newWorld(t, []string{"bin"}, nil, nil)
	w.applet(0)
	// a squatter registers the core's concrete type under another id;
	// the core's own registration then collides on the concrete type
	w.rt.reg.Register("myintro", &Introspector{}, registry.Options{})
	if code := run(w.rt); code != 2 {
		t.Errorf("exit = %d, want 2; squatting must fail startup", code)
	}
	if !strings.Contains(w.stderr.String(), "already registered") {
		t.Errorf("expected the duplicate concrete type violation:\n%s", w.stderr.String())
	}
}
