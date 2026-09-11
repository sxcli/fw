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
	"strings"
	"testing"

	"sxcli.dev/conf/fail"
)

// Applets are not addressable by controls — the dispatched one
// included. The old "applet is disabled" special case died with the
// one-applet rule that made it possible.
func TestDisablingDispatchedAppletFails(t *testing.T) {
	w := newWorld(t, []string{"bin", "--disable", "app"}, nil, nil)
	w.applet(0)
	if code := w.run(); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(w.stderr.String(), `disable: unknown service "app"`) {
		t.Errorf("an applet must be unknown to controls:\n%s", w.stderr.String())
	}
}

// The core is a virtual root, not a registry entry; introspection
// synthesizes its visibility.
func TestIntrospectionSynthesizesCore(t *testing.T) {
	w := newWorld(t, []string{"bin"}, nil, nil)
	w.applet(0)
	if err := w.build(); err != nil {
		t.Fatalf("build failed: %v", err)
	}
	c := &fail.Collector{}
	ca := &catalog{reg: w.rt.reg}
	ca.index(c)
	i := ca.introspector("app")
	if i == nil || c.Len() != 0 {
		t.Fatalf("the applet view must construct: %v", c.All())
	}
	services := i.Services()
	if len(services) == 0 || services[0] != "core" {
		t.Errorf("Services must lead with the synthesized core: %v", services)
	}
	if i.Describe("core") == "" {
		t.Error("Describe(core) must answer")
	}
}

// The core node itself is inert: it joins the resolved service set
// but has no
// lifecycle, so the run is exactly what it was before it existed.
func TestCoreNodeIsLifecycleInert(t *testing.T) {
	w := newWorld(t, []string{"bin"}, nil, nil)
	w.applet(0)
	w.dep(false)
	w.cat.All()[0].Deps[0].Optional = false
	if code := w.run(); code != 0 {
		t.Fatalf("exit %d; stderr:\n%s", code, w.stderr.String())
	}
	want := "dep.configured,applet.configured,dep.start,applet.run,dep.stop"
	if strings.Join(w.log, ",") != want {
		t.Errorf("lifecycle changed by the core node: %v", w.log)
	}
}
