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
	"reflect"
	"testing"

	"sxcli.dev/conf/fail"
	"sxcli.dev/fw/internal/graph"
	"sxcli.dev/fw/internal/registry"
	"sxcli.dev/rules/solver"
)

type corePinApplet struct{ N int }

// TestCoreRootPinsSolverShape renders the runtime's virtual core root
// through the graph's solver vocabulary and asserts it equals
// solver.CoreRoot for the same inputs. The shape is authored in
// sxcli.dev/rules; this test is the tripwire — if coreRoot ever grows
// or changes a field, it goes red until rules learns the same shape,
// so the runtime and sxcli-vet's mirror cannot drift apart silently.
func TestCoreRootPinsSolverShape(t *testing.T) {
	c := &fail.Collector{}
	ca := catalog{reg: registry.New(c)}
	d := &registry.Descriptor{
		ID:       "t/app",
		Concrete: reflect.TypeOf(&corePinApplet{}),
	}
	root := ca.coreRoot(c, d, []string{"sxcli.dev/fw/configfmt/yaml"})
	if c.Len() != 0 {
		t.Fatalf("building the core root failed: %v", c.All())
	}
	got := graph.RenderMember(root)
	// the root's concrete identity is the per-invocation dynamic
	// struct — unnameable, and nothing depends on the root, so it
	// carries no information the pin needs
	got.Concrete = ""
	want := solver.CoreRoot(solver.CoreRootSpec{
		RootID:      "core",
		AppletID:    "t/app",
		AppletType:  "*sxcli.dev/fw.corePinApplet",
		Translator:  "sxcli.dev/fw.Translator",
		Provider:    "sxcli.dev/fw.ConfigFormatProvider",
		ProviderIDs: []string{"sxcli.dev/fw/configfmt/yaml"},
	})
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the runtime's core root and solver.CoreRoot disagree:\n got  %+v\n want %+v", got, want)
	}
}
