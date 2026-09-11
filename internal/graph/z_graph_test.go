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

package graph

import (
	"reflect"
	"strings"
	"testing"

	"sxcli.dev/conf/fail"
	"sxcli.dev/fw/internal/registry"
)

func newRegistry() *registry.Registry {
	return registry.New(&fail.Collector{})
}

type worker interface{ Work() }
type storage interface{ Store() }

type app struct {
	W worker `inject:""`
}

type appByID struct {
	W worker `inject:"t/workerb"`
}

type appOptional struct {
	W worker `inject:";optional"`
}

type appAll struct {
	Ws []worker `inject:""`
}

type appSeeded struct {
	Ws []worker `inject:"t/workera"`
}

type workerA struct{}

func (w *workerA) Work() {}

type workerB struct {
	S storage `inject:""`
}

func (w *workerB) Work() {}

type storeA struct{}

func (s *storeA) Store() {}

type storeB struct{}

func (s *storeB) Store() {}

type appStore struct {
	S *storeA `inject:""`
}

type ping struct {
	Peer storage `inject:""`
}

func (p *ping) Work() {}

type pong struct {
	Peer worker `inject:""`
}

func (p *pong) Store() {}

type selfish struct {
	Me worker `inject:""`
}

func (s *selfish) Work() {}

var workerType = reflect.TypeOf((*worker)(nil)).Elem()
var storageType = reflect.TypeOf((*storage)(nil)).Elem()

// reg commits instance under id the way the root's chain does: the
// descriptor arrives with identity validated and Provides verified.
func reg(r *registry.Registry, id string, instance any, provides ...reflect.Type) *registry.Descriptor {
	d := &registry.Descriptor{ID: id, Instance: instance, Concrete: reflect.TypeOf(instance), Alias: id, Provides: provides}
	r.Commit(d)
	return d
}

func ids(res Result) []string {
	var out []string
	for _, m := range res.Ordered {
		out = append(out, m.Desc.ID)
	}
	return out
}

func position(t *testing.T, res Result, id string) int {
	t.Helper()
	found := -1
	for i, m := range res.Ordered {
		if m.Desc.ID == id {
			found = i
		}
	}
	if found < 0 {
		t.Fatalf("%q not in resolved order %v", id, ids(res))
	}
	return found
}

func mustResolve(t *testing.T, reg *registry.Registry, rootID string, ctl Controls) Result {
	t.Helper()
	root, ok := reg.ByID(rootID)
	if !ok {
		t.Fatalf("root %q is not registered", rootID)
	}
	return mustResolveRoot(t, reg, root, ctl)
}

func mustResolveRoot(t *testing.T, reg *registry.Registry, root *registry.Descriptor, ctl Controls) Result {
	t.Helper()
	c := &fail.Collector{}
	res := Resolve(c, reg, root, ctl)
	if c.Len() != 0 {
		t.Fatalf("unexpected resolve errors: %v", c.All())
	}
	return res
}

func TestChainOrderAndBindings(t *testing.T) {
	r := newRegistry()
	reg(r, "t/app", &app{})
	reg(r, "t/workerb", &workerB{}, workerType)
	reg(r, "t/storea", &storeA{}, storageType)
	res := mustResolve(t, r, "t/app", Controls{})
	if len(res.Ordered) != 3 || len(res.Cycles) != 0 {
		t.Fatalf("got order %v, cycles %v", ids(res), res.Cycles)
	}
	if !(position(t, res, "t/storea") < position(t, res, "t/workerb") && position(t, res, "t/workerb") < position(t, res, "t/app")) {
		t.Errorf("dependency order violated: %v", ids(res))
	}
	m := res.Ordered[position(t, res, "t/app")]
	if len(m.Bindings) != 1 || len(m.Bindings[0].Targets) != 1 || m.Bindings[0].Targets[0].ID != "t/workerb" {
		t.Errorf("app binding wrong: %+v", m.Bindings)
	}
}

func TestColdServicesStayOut(t *testing.T) {
	r := newRegistry()
	reg(r, "t/app", &app{})
	reg(r, "t/workera", &workerA{}, workerType)
	reg(r, "t/storea", &storeA{}, storageType) // nothing pulls it
	res := mustResolve(t, r, "t/app", Controls{})
	if len(res.Ordered) != 2 {
		t.Errorf("cold service leaked into the resolved service set: %v", ids(res))
	}
}

// The old TestFirstRegisteredWins is consciously retired: silent
// first-registered tie-breaking was the import-order hazard the
// composition release outlawed. Its two successors:

func TestRankedWinsTie(t *testing.T) {
	r := newRegistry()
	reg(r, "t/app", &app{})
	reg(r, "t/workera", &workerA{}, workerType)
	reg(r, "t/workerb", &workerB{}, workerType)
	reg(r, "t/storea", &storeA{}, storageType)
	first, _ := r.ByID("t/workera")
	first.Ranked = true // what Build sets for Order-listed members
	res := mustResolve(t, r, "t/app", Controls{})
	m := res.Ordered[position(t, res, "t/app")]
	if m.Bindings[0].Targets[0].ID != "t/workera" {
		t.Errorf("the ranked candidate must win, got %q", m.Bindings[0].Targets[0].ID)
	}
	if len(res.Ordered) != 2 {
		t.Errorf("only the winner should join the resolved service set: %v", ids(res))
	}
}

func TestUnrankedTieIsViolation(t *testing.T) {
	r := newRegistry()
	reg(r, "t/app", &app{})
	reg(r, "t/workera", &workerA{}, workerType)
	reg(r, "t/workerb", &workerB{}, workerType)
	reg(r, "t/storea", &storeA{}, storageType)
	c := &fail.Collector{}
	root, _ := r.ByID("t/app")
	Resolve(c, r, root, Controls{})
	if c.Len() == 0 {
		t.Fatal("an unranked single-valued tie must be a violation")
	}
	msg := c.All()[0].Error()
	if !strings.Contains(msg, "ambiguous") || !strings.Contains(msg, `"t/workera"`) || !strings.Contains(msg, `"t/workerb"`) || !strings.Contains(msg, "sxcli-vet") {
		t.Errorf("the violation must name both candidates and point at the vet tool: %s", msg)
	}
}

func TestSliceGathersLateJoiners(t *testing.T) {
	r := newRegistry()
	reg(r, "t/appseeded", &appSeeded{})
	reg(r, "t/workera", &workerA{}, workerType)
	reg(r, "t/workerb", &workerB{}, workerType) // joins via Enable, not via injection
	reg(r, "t/storea", &storeA{}, storageType)
	res := mustResolve(t, r, "t/appseeded", Controls{Enable: []string{"t/workerb"}})
	m := res.Ordered[position(t, res, "t/appseeded")]
	var got []string
	for _, target := range m.Bindings[0].Targets {
		got = append(got, target.ID)
	}
	if !reflect.DeepEqual(got, []string{"t/workera", "t/workerb"}) {
		t.Errorf("slice must gather every match in the resolved service set in registration order, got %v", got)
	}
}

func TestBareSlicePullsAllRegistered(t *testing.T) {
	r := newRegistry()
	reg(r, "t/appall", &appAll{})
	reg(r, "t/workera", &workerA{}, workerType)
	reg(r, "t/workerb", &workerB{}, workerType)
	reg(r, "t/storea", &storeA{}, storageType)
	res := mustResolve(t, r, "t/appall", Controls{})
	if len(res.Ordered) != 4 {
		t.Errorf("bare slice must pull every registered match: %v", ids(res))
	}
}

func TestOptionalMissingIsFine(t *testing.T) {
	r := newRegistry()
	reg(r, "t/appopt", &appOptional{})
	res := mustResolve(t, r, "t/appopt", Controls{})
	m := res.Ordered[0]
	if len(m.Bindings) != 1 || len(m.Bindings[0].Targets) != 0 {
		t.Errorf("optional unmatched field must bind empty: %+v", m.Bindings)
	}
}

func TestResolutionErrors(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(r *registry.Registry)
		applet string
		ctl    Controls
	}{
		{"required dependency missing", func(r *registry.Registry) {
			reg(r, "t/app", &app{})
		}, "t/app", Controls{}},
		{"unknown id in tag", func(r *registry.Registry) {
			reg(r, "t/appbyid", &appByID{})
		}, "t/appbyid", Controls{}},
		// "unknown applet" and "disabled applet" moved out of the
		// graph: the root arrives as a descriptor, so existence and
		// the human disabled-message are the root package's job now
		{"disabled required by-id dependency", func(r *registry.Registry) {
			reg(r, "t/appbyid", &appByID{})
			reg(r, "t/workerb", &workerB{}, workerType)
			reg(r, "t/storea", &storeA{}, storageType)
		}, "t/appbyid", Controls{Disable: []string{"t/workerb"}}},
		{"disable unknown id", func(r *registry.Registry) {
			reg(r, "t/app", &app{})
			reg(r, "t/workera", &workerA{}, workerType)
		}, "t/app", Controls{Disable: []string{"ghost"}}},
		{"enable unknown id", func(r *registry.Registry) {
			reg(r, "t/app", &app{})
			reg(r, "t/workera", &workerA{}, workerType)
		}, "t/app", Controls{Enable: []string{"ghost"}}},
		{"enabled and disabled", func(r *registry.Registry) {
			reg(r, "t/app", &app{})
			reg(r, "t/workera", &workerA{}, workerType)
		}, "t/app", Controls{Enable: []string{"t/workera"}, Disable: []string{"t/workera"}}},
		{"override to unknown substitute", func(r *registry.Registry) {
			reg(r, "t/app", &app{})
			reg(r, "t/workera", &workerA{}, workerType)
		}, "t/app", Controls{Override: map[string]string{"t/workera": "ghost"}}},
		{"override type mismatch", func(r *registry.Registry) {
			reg(r, "t/appbyid", &appByID{})
			reg(r, "t/workerb", &workerB{}, workerType)
			reg(r, "t/storea", &storeA{}, storageType)
		}, "t/appbyid", Controls{Override: map[string]string{"t/workerb": "t/storea"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRegistry()
			tc.setup(r)
			root, ok := r.ByID(tc.applet)
			if !ok {
				t.Fatalf("root %q is not registered", tc.applet)
			}
			c := &fail.Collector{}
			Resolve(c, r, root, tc.ctl)
			if c.Len() == 0 {
				t.Error("expected resolve errors, got none")
			}
		})
	}
}

func TestDisableSteersBareField(t *testing.T) {
	r := newRegistry()
	reg(r, "t/app", &app{})
	reg(r, "t/workera", &workerA{}, workerType)
	reg(r, "t/workerb", &workerB{}, workerType)
	reg(r, "t/storea", &storeA{}, storageType)
	res := mustResolve(t, r, "t/app", Controls{Disable: []string{"t/workera"}})
	m := res.Ordered[position(t, res, "t/app")]
	if m.Bindings[0].Targets[0].ID != "t/workerb" {
		t.Errorf("disable must steer to the next candidate, got %q", m.Bindings[0].Targets[0].ID)
	}
}

func TestOverrideSubstitutes(t *testing.T) {
	r := newRegistry()
	reg(r, "t/appbyid", &appByID{})
	reg(r, "t/workera", &workerA{}, workerType)
	reg(r, "t/workerb", &workerB{}, workerType)
	reg(r, "t/storea", &storeA{}, storageType)
	res := mustResolve(t, r, "t/appbyid", Controls{Disable: []string{"t/workerb"}, Override: map[string]string{"t/workerb": "t/workera"}})
	m := res.Ordered[position(t, res, "t/appbyid")]
	if m.Bindings[0].Targets[0].ID != "t/workera" {
		t.Errorf("override must substitute, got %q", m.Bindings[0].Targets[0].ID)
	}
	if len(res.UnusedOverrides) != 0 {
		t.Errorf("a fired override must not be reported unused: %v", res.UnusedOverrides)
	}
	for _, member := range res.Ordered {
		if member.Desc.ID == "t/workerb" || member.Desc.ID == "t/storea" {
			t.Errorf("substituted-away service leaked into the resolved service set: %v", ids(res))
		}
	}
}

func TestUnusedOverridesAreReported(t *testing.T) {
	r := newRegistry()
	reg(r, "t/app", &app{})
	reg(r, "t/workera", &workerA{}, workerType)
	res := mustResolve(t, r, "t/app", Controls{Override: map[string]string{
		"ghost":   "t/workera", // unregistered key: legal rescue mapping, but unused here
		"unfired": "t/workera",
	}})
	if strings.Join(res.UnusedOverrides, ",") != "ghost,unfired" {
		t.Errorf("unused overrides must be reported sorted: %v", res.UnusedOverrides)
	}
}

func TestEnableForcesColdService(t *testing.T) {
	r := newRegistry()
	reg(r, "t/app", &app{})
	reg(r, "t/workera", &workerA{}, workerType)
	reg(r, "t/workerb", &workerB{}, workerType) // cold unless enabled; drags storea
	reg(r, "t/storea", &storeA{}, storageType)
	first, _ := r.ByID("t/workera")
	first.Ranked = true // resolve the tie the composed way; the test is about Enable
	res := mustResolve(t, r, "t/app", Controls{Enable: []string{"t/workerb"}})
	if len(res.Ordered) != 4 {
		t.Errorf("enable must pull the service and its deps: %v", ids(res))
	}
	if !(position(t, res, "t/storea") < position(t, res, "t/workerb")) {
		t.Errorf("enabled service must still start after its deps: %v", ids(res))
	}
}

func TestConcreteTypeDependency(t *testing.T) {
	r := newRegistry()
	reg(r, "t/appstore", &appStore{})
	reg(r, "t/storea", &storeA{}, storageType)
	res := mustResolve(t, r, "t/appstore", Controls{})
	m := res.Ordered[position(t, res, "t/appstore")]
	if m.Bindings[0].Targets[0].ID != "t/storea" {
		t.Errorf("concrete type dependency not resolved: %+v", m.Bindings)
	}
}

func TestCycleIsWarningNotError(t *testing.T) {
	r := newRegistry()
	reg(r, "t/ping", &ping{}, workerType)
	reg(r, "t/pong", &pong{}, storageType)
	res := mustResolve(t, r, "t/ping", Controls{})
	if len(res.Ordered) != 2 {
		t.Fatalf("cycle members must stay in the resolved service set: %v", ids(res))
	}
	if !reflect.DeepEqual(res.Cycles, [][]string{{"t/ping", "t/pong"}}) {
		t.Errorf("cycle not reported: %v", res.Cycles)
	}
	if !reflect.DeepEqual(ids(res), []string{"t/ping", "t/pong"}) {
		t.Errorf("within a cycle registration order applies: %v", ids(res))
	}
}

func TestSelfLoopIsReported(t *testing.T) {
	r := newRegistry()
	reg(r, "t/selfish", &selfish{}, workerType)
	res := mustResolve(t, r, "t/selfish", Controls{})
	if !reflect.DeepEqual(res.Cycles, [][]string{{"t/selfish"}}) {
		t.Errorf("self-loop not reported: %v", res.Cycles)
	}
}

// virtualRoot mirrors the framework's core node at graph level: a
// required by-id edge (the applet) and an optional by-id edge (a
// translator, a provider in use).
type virtualRoot struct {
	A *app    `inject:"t/app"`
	S storage `inject:"t/storea;optional"`
}

func TestVirtualRootEdgesJoinAndDisabledOptionalSkips(t *testing.T) {
	r := newRegistry()
	reg(r, "t/app", &app{})
	reg(r, "t/workera", &workerA{}, workerType)
	reg(r, "t/storea", &storeA{}, storageType)
	root := r.Virtual("core", &virtualRoot{}, &fail.Collector{})
	res := mustResolveRoot(t, r, root, Controls{})
	if len(res.Ordered) != 4 {
		t.Errorf("root edges must join the resolved service set: %v", ids(res))
	}
	if res.Ordered[len(res.Ordered)-1].Desc.ID != "core" {
		t.Errorf("the root depends on everything and must order last: %v", ids(res))
	}
	res = mustResolveRoot(t, r, root, Controls{Disable: []string{"t/storea"}})
	if len(res.Ordered) != 3 {
		t.Errorf("a disabled optional edge must drop silently: %v", ids(res))
	}
}

func TestDiamondResolvesOnce(t *testing.T) {
	r := newRegistry()
	reg(r, "t/appall", &appAll{})
	reg(r, "t/ping", &ping{}, workerType)
	reg(r, "t/workerb", &workerB{}, workerType) // both need storage
	reg(r, "t/storea", &storeA{}, storageType)
	res := mustResolve(t, r, "t/appall", Controls{})
	if len(res.Ordered) != 4 {
		t.Fatalf("diamond dependency duplicated or lost: %v", ids(res))
	}
	if !(position(t, res, "t/storea") < position(t, res, "t/ping") && position(t, res, "t/storea") < position(t, res, "t/workerb")) {
		t.Errorf("shared dependency must precede both dependents: %v", ids(res))
	}
}

func TestSubtreeWalksBindings(t *testing.T) {
	r := newRegistry()
	reg(r, "t/app", &app{})
	reg(r, "t/workerb", &workerB{}, workerType) // needs storage
	reg(r, "t/storea", &storeA{}, storageType)
	root := r.Virtual("core", &virtualRoot{}, &fail.Collector{})
	res := mustResolveRoot(t, r, root, Controls{})
	sub, ok := res.Subtree("t/workerb")
	if !ok {
		t.Fatal("workerb is in the resolved service set")
	}
	if len(sub.Ordered) != 2 || sub.Ordered[0].Desc.ID != "t/storea" || sub.Ordered[1].Desc.ID != "t/workerb" {
		t.Errorf("subtree must be the reachable set in dependency order: %v", ids(sub))
	}
	if _, ok := res.Subtree("ghost"); ok {
		t.Error("a non-member must report ok=false")
	}
}
