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
	"strings"
	"testing"
)

// System applets must not count toward single-applet mode: the main
// applet still owns the whole argument vector.
func TestSystemAppletKeepsSingleAppletMode(t *testing.T) {
	w := newWorld(t, []string{"bin", "--greeting=hi"}, nil, nil)
	a := w.applet(0)
	w.rt.reg.Register("second", &secondApplet{log: &w.log}, foldOptions([]RegisterOption{System()}))
	if code := run(w.rt); code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, w.stderr.String())
	}
	if a.cfg.Greeting != "hi" {
		t.Errorf("single-applet mode broken by System applet: %q", a.cfg.Greeting)
	}
	if strings.Join(w.log, ",") != "applet.configured,applet.run" {
		t.Errorf("main applet did not run: %v", w.log)
	}
}

// A first bare token naming a System applet selects it — even in
// single-applet mode. This is the tooling entry path.
func TestSystemAppletSelectableInSingleAppletMode(t *testing.T) {
	w := newWorld(t, []string{"bin", "second"}, nil, nil)
	w.applet(0)
	w.rt.reg.Register("second", &secondApplet{log: &w.log}, foldOptions([]RegisterOption{System()}))
	if code := run(w.rt); code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, w.stderr.String())
	}
	if strings.Join(w.log, ",") != "second.run" {
		t.Errorf("System selector did not dispatch: %v", w.log)
	}
}

// A genuine positional colliding with a System id uses the standard
// leading -- escape and stays applet data.
func TestSystemIdCollisionEscapedByDashDash(t *testing.T) {
	w := newWorld(t, []string{"bin", "--", "second"}, nil, nil)
	w.applet(0)
	w.rt.reg.Register("second", &secondApplet{log: &w.log}, foldOptions([]RegisterOption{System()}))
	if code := run(w.rt); code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, w.stderr.String())
	}
	if strings.Join(w.log, ",") != "applet.configured,applet.run" {
		t.Errorf("escaped positional still dispatched: %v", w.log)
	}
	if strings.Join(Positionals(), ",") != "second" {
		t.Errorf("positionals wrong: %v", Positionals())
	}
}

// Hidden applets stay selectable by explicit first token.
func TestHiddenAppletExplicitSelectorWorks(t *testing.T) {
	w := newWorld(t, []string{"bin", "second"}, nil, nil)
	w.applet(0)
	w.rt.reg.Register("second", &secondApplet{log: &w.log}, foldOptions([]RegisterOption{Hidden()}))
	if code := run(w.rt); code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, w.stderr.String())
	}
	if strings.Join(w.log, ",") != "second.run" {
		t.Errorf("Hidden selector did not dispatch: %v", w.log)
	}
}

// Hidden applets are absent from the dispatch-failure usage list. A
// Hidden non-System applet still counts, so this world is multi-applet.
func TestHiddenAppletExcludedFromUsage(t *testing.T) {
	w := newWorld(t, []string{"bin", "ghost"}, nil, nil)
	w.applet(0)
	w.rt.reg.Register("second", &secondApplet{log: &w.log}, foldOptions([]RegisterOption{Hidden()}))
	if code := run(w.rt); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	text := w.stderr.String()
	if !strings.Contains(text, "app") || strings.Contains(text, "second") {
		t.Errorf("usage list wrong:\n%s", text)
	}
}

// Basename dispatch never matches a Hidden applet.
func TestHiddenAppletNotMatchedByBasename(t *testing.T) {
	w := newWorld(t, []string{"/usr/bin/second"}, nil, nil)
	w.applet(0)
	w.rt.reg.Register("second", &secondApplet{log: &w.log}, foldOptions([]RegisterOption{Hidden()}))
	if code := run(w.rt); code != 2 {
		t.Fatalf("exit code = %d, want 2; log: %v", code, w.log)
	}
	if !strings.Contains(w.stderr.String(), "does not name an applet") {
		t.Errorf("usage dump wrong:\n%s", w.stderr.String())
	}
}

// Hidden/System on a service that is not an applet is a registration
// violation.
func TestVisibilityOnNonAppletFails(t *testing.T) {
	w := newWorld(t, []string{"bin"}, nil, nil)
	w.applet(0)
	w.rt.reg.Register("bare", &plainService{}, foldOptions([]RegisterOption{Hidden()}))
	if code := run(w.rt); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(w.stderr.String(), "apply only to applets") {
		t.Errorf("violation not reported:\n%s", w.stderr.String())
	}
}

// The Introspector reports public applets only; System implies Hidden,
// so the folded options alone must hide it.
func TestIntrospectorAppletsOmitHiddenAndSystem(t *testing.T) {
	w := newWorld(t, []string{"bin"}, nil, nil)
	w.applet(0)
	w.rt.reg.Register("second", &secondApplet{log: &w.log}, foldOptions([]RegisterOption{System()}))
	i := &Introspector{rt: w.rt}
	if got := strings.Join(i.Applets(), ","); got != "app" {
		t.Errorf("Applets() = %q, want %q", got, "app")
	}
}
