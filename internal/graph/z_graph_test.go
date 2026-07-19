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

	"sxcli.dev/fw/internal/fail"
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
	W worker `inject:"workerb"`
}

type appOptional struct {
	W worker `inject:";optional"`
}

type appAll struct {
	Ws []worker `inject:""`
}

type appSeeded struct {
	Ws []worker `inject:"workera"`
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
	d := &registry.Descriptor{ID: id, Instance: instance, Concrete: reflect.TypeOf(instance), Aliases: []string{id}, Provides: provides}
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
	reg(r, "app", &app{})
	reg(r, "workerb", &workerB{}, workerType)
	reg(r, "storea", &storeA{}, storageType)
	res := mustResolve(t, r, "app", Controls{})
	if len(res.Ordered) != 3 || len(res.Cycles) != 0 {
		t.Fatalf("got order %v, cycles %v", ids(res), res.Cycles)
	}
	if !(position(t, res, "storea") < position(t, res, "workerb") && position(t, res, "workerb") < position(t, res, "app")) {
		t.Errorf("dependency order violated: %v", ids(res))
	}
	m := res.Ordered[position(t, res, "app")]
	if len(m.Bindings) != 1 || len(m.Bindings[0].Targets) != 1 || m.Bindings[0].Targets[0].ID != "workerb" {
		t.Errorf("app binding wrong: %+v", m.Bindings)
	}
}

func TestColdServicesStayOut(t *testing.T) {
	r := newRegistry()
	reg(r, "app", &app{})
	reg(r, "workera", &workerA{}, workerType)
	reg(r, "storea", &storeA{}, storageType) // nothing pulls it
	res := mustResolve(t, r, "app", Controls{})
	if len(res.Ordered) != 2 {
		t.Errorf("cold service leaked into closure: %v", ids(res))
	}
}

// The old TestFirstRegisteredWins is consciously retired: silent
// first-registered tie-breaking was the import-order hazard the
// composition release outlawed. Its two successors:

func TestRankedWinsTie(t *testing.T) {
	r := newRegistry()
	reg(r, "app", &app{})
	reg(r, "workera", &workerA{}, workerType)
	reg(r, "workerb", &workerB{}, workerType)
	reg(r, "storea", &storeA{}, storageType)
	first, _ := r.ByID("workera")
	first.Ranked = true // what Build sets for Order-listed members
	res := mustResolve(t, r, "app", Controls{})
	m := res.Ordered[position(t, res, "app")]
	if m.Bindings[0].Targets[0].ID != "workera" {
		t.Errorf("the ranked candidate must win, got %q", m.Bindings[0].Targets[0].ID)
	}
	if len(res.Ordered) != 2 {
		t.Errorf("only the winner should join the closure: %v", ids(res))
	}
}

func TestUnrankedTieIsViolation(t *testing.T) {
	r := newRegistry()
	reg(r, "app", &app{})
	reg(r, "workera", &workerA{}, workerType)
	reg(r, "workerb", &workerB{}, workerType)
	reg(r, "storea", &storeA{}, storageType)
	c := &fail.Collector{}
	root, _ := r.ByID("app")
	Resolve(c, r, root, Controls{})
	if c.Len() == 0 {
		t.Fatal("an unranked single-valued tie must be a violation")
	}
	msg := c.All()[0].Error()
	if !strings.Contains(msg, "ambiguous") || !strings.Contains(msg, `"workera"`) || !strings.Contains(msg, `"workerb"`) || !strings.Contains(msg, "sxcli-vet") {
		t.Errorf("the violation must name both candidates and point at the vet tool: %s", msg)
	}
}

func TestSliceGathersLateJoiners(t *testing.T) {
	r := newRegistry()
	reg(r, "appseeded", &appSeeded{})
	reg(r, "workera", &workerA{}, workerType)
	reg(r, "workerb", &workerB{}, workerType) // joins via Enable, not via injection
	reg(r, "storea", &storeA{}, storageType)
	res := mustResolve(t, r, "appseeded", Controls{Enable: []string{"workerb"}})
	m := res.Ordered[position(t, res, "appseeded")]
	var got []string
	for _, target := range m.Bindings[0].Targets {
		got = append(got, target.ID)
	}
	if !reflect.DeepEqual(got, []string{"workera", "workerb"}) {
		t.Errorf("slice must gather every closure match in registration order, got %v", got)
	}
}

func TestBareSlicePullsAllRegistered(t *testing.T) {
	r := newRegistry()
	reg(r, "appall", &appAll{})
	reg(r, "workera", &workerA{}, workerType)
	reg(r, "workerb", &workerB{}, workerType)
	reg(r, "storea", &storeA{}, storageType)
	res := mustResolve(t, r, "appall", Controls{})
	if len(res.Ordered) != 4 {
		t.Errorf("bare slice must pull every registered match: %v", ids(res))
	}
}

func TestOptionalMissingIsFine(t *testing.T) {
	r := newRegistry()
	reg(r, "appopt", &appOptional{})
	res := mustResolve(t, r, "appopt", Controls{})
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
			reg(r, "app", &app{})
		}, "app", Controls{}},
		{"unknown id in tag", func(r *registry.Registry) {
			reg(r, "appbyid", &appByID{})
		}, "appbyid", Controls{}},
		// "unknown applet" and "disabled applet" moved out of the
		// graph: the root arrives as a descriptor, so existence and
		// the human disabled-message are the root package's job now
		{"disabled required by-id dependency", func(r *registry.Registry) {
			reg(r, "appbyid", &appByID{})
			reg(r, "workerb", &workerB{}, workerType)
			reg(r, "storea", &storeA{}, storageType)
		}, "appbyid", Controls{Disable: []string{"workerb"}}},
		{"disable unknown id", func(r *registry.Registry) {
			reg(r, "app", &app{})
			reg(r, "workera", &workerA{}, workerType)
		}, "app", Controls{Disable: []string{"ghost"}}},
		{"enable unknown id", func(r *registry.Registry) {
			reg(r, "app", &app{})
			reg(r, "workera", &workerA{}, workerType)
		}, "app", Controls{Enable: []string{"ghost"}}},
		{"enabled and disabled", func(r *registry.Registry) {
			reg(r, "app", &app{})
			reg(r, "workera", &workerA{}, workerType)
		}, "app", Controls{Enable: []string{"workera"}, Disable: []string{"workera"}}},
		{"override to unknown substitute", func(r *registry.Registry) {
			reg(r, "app", &app{})
			reg(r, "workera", &workerA{}, workerType)
		}, "app", Controls{Override: map[string]string{"workera": "ghost"}}},
		{"override type mismatch", func(r *registry.Registry) {
			reg(r, "appbyid", &appByID{})
			reg(r, "workerb", &workerB{}, workerType)
			reg(r, "storea", &storeA{}, storageType)
		}, "appbyid", Controls{Override: map[string]string{"workerb": "storea"}}},
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
	reg(r, "app", &app{})
	reg(r, "workera", &workerA{}, workerType)
	reg(r, "workerb", &workerB{}, workerType)
	reg(r, "storea", &storeA{}, storageType)
	res := mustResolve(t, r, "app", Controls{Disable: []string{"workera"}})
	m := res.Ordered[position(t, res, "app")]
	if m.Bindings[0].Targets[0].ID != "workerb" {
		t.Errorf("disable must steer to the next candidate, got %q", m.Bindings[0].Targets[0].ID)
	}
}

func TestOverrideSubstitutes(t *testing.T) {
	r := newRegistry()
	reg(r, "appbyid", &appByID{})
	reg(r, "workera", &workerA{}, workerType)
	reg(r, "workerb", &workerB{}, workerType)
	reg(r, "storea", &storeA{}, storageType)
	res := mustResolve(t, r, "appbyid", Controls{Disable: []string{"workerb"}, Override: map[string]string{"workerb": "workera"}})
	m := res.Ordered[position(t, res, "appbyid")]
	if m.Bindings[0].Targets[0].ID != "workera" {
		t.Errorf("override must substitute, got %q", m.Bindings[0].Targets[0].ID)
	}
	if len(res.UnusedOverrides) != 0 {
		t.Errorf("a fired override must not be reported unused: %v", res.UnusedOverrides)
	}
	for _, member := range res.Ordered {
		if member.Desc.ID == "workerb" || member.Desc.ID == "storea" {
			t.Errorf("substituted-away service leaked into closure: %v", ids(res))
		}
	}
}

func TestUnusedOverridesAreReported(t *testing.T) {
	r := newRegistry()
	reg(r, "app", &app{})
	reg(r, "workera", &workerA{}, workerType)
	res := mustResolve(t, r, "app", Controls{Override: map[string]string{
		"ghost":   "workera", // unregistered key: legal rescue mapping, but unused here
		"unfired": "workera",
	}})
	if strings.Join(res.UnusedOverrides, ",") != "ghost,unfired" {
		t.Errorf("unused overrides must be reported sorted: %v", res.UnusedOverrides)
	}
}

func TestEnableForcesColdService(t *testing.T) {
	r := newRegistry()
	reg(r, "app", &app{})
	reg(r, "workera", &workerA{}, workerType)
	reg(r, "workerb", &workerB{}, workerType) // cold unless enabled; drags storea
	reg(r, "storea", &storeA{}, storageType)
	first, _ := r.ByID("workera")
	first.Ranked = true // resolve the tie the composed way; the test is about Enable
	res := mustResolve(t, r, "app", Controls{Enable: []string{"workerb"}})
	if len(res.Ordered) != 4 {
		t.Errorf("enable must pull the service and its deps: %v", ids(res))
	}
	if !(position(t, res, "storea") < position(t, res, "workerb")) {
		t.Errorf("enabled service must still start after its deps: %v", ids(res))
	}
}

func TestConcreteTypeDependency(t *testing.T) {
	r := newRegistry()
	reg(r, "appstore", &appStore{})
	reg(r, "storea", &storeA{}, storageType)
	res := mustResolve(t, r, "appstore", Controls{})
	m := res.Ordered[position(t, res, "appstore")]
	if m.Bindings[0].Targets[0].ID != "storea" {
		t.Errorf("concrete type dependency not resolved: %+v", m.Bindings)
	}
}

func TestCycleIsWarningNotError(t *testing.T) {
	r := newRegistry()
	reg(r, "ping", &ping{}, workerType)
	reg(r, "pong", &pong{}, storageType)
	res := mustResolve(t, r, "ping", Controls{})
	if len(res.Ordered) != 2 {
		t.Fatalf("cycle members must stay in the closure: %v", ids(res))
	}
	if !reflect.DeepEqual(res.Cycles, [][]string{{"ping", "pong"}}) {
		t.Errorf("cycle not reported: %v", res.Cycles)
	}
	if !reflect.DeepEqual(ids(res), []string{"ping", "pong"}) {
		t.Errorf("within a cycle registration order applies: %v", ids(res))
	}
}

func TestSelfLoopIsReported(t *testing.T) {
	r := newRegistry()
	reg(r, "selfish", &selfish{}, workerType)
	res := mustResolve(t, r, "selfish", Controls{})
	if !reflect.DeepEqual(res.Cycles, [][]string{{"selfish"}}) {
		t.Errorf("self-loop not reported: %v", res.Cycles)
	}
}

// virtualRoot mirrors the framework's core node at graph level: a
// required by-id edge (the applet) and an optional by-id edge (a
// translator, a provider in use).
type virtualRoot struct {
	A *app    `inject:"app"`
	S storage `inject:"storea;optional"`
}

func TestVirtualRootEdgesJoinAndDisabledOptionalSkips(t *testing.T) {
	r := newRegistry()
	reg(r, "app", &app{})
	reg(r, "workera", &workerA{}, workerType)
	reg(r, "storea", &storeA{}, storageType)
	root := r.Virtual("core", &virtualRoot{}, &fail.Collector{})
	res := mustResolveRoot(t, r, root, Controls{})
	if len(res.Ordered) != 4 {
		t.Errorf("root edges must join the closure: %v", ids(res))
	}
	if res.Ordered[len(res.Ordered)-1].Desc.ID != "core" {
		t.Errorf("the root depends on everything and must order last: %v", ids(res))
	}
	res = mustResolveRoot(t, r, root, Controls{Disable: []string{"storea"}})
	if len(res.Ordered) != 3 {
		t.Errorf("a disabled optional edge must drop silently: %v", ids(res))
	}
}

func TestDiamondResolvesOnce(t *testing.T) {
	r := newRegistry()
	reg(r, "appall", &appAll{})
	reg(r, "ping", &ping{}, workerType)
	reg(r, "workerb", &workerB{}, workerType) // both need storage
	reg(r, "storea", &storeA{}, storageType)
	res := mustResolve(t, r, "appall", Controls{})
	if len(res.Ordered) != 4 {
		t.Fatalf("diamond dependency duplicated or lost: %v", ids(res))
	}
	if !(position(t, res, "storea") < position(t, res, "ping") && position(t, res, "storea") < position(t, res, "workerb")) {
		t.Errorf("shared dependency must precede both dependents: %v", ids(res))
	}
}

func TestSubtreeWalksBindings(t *testing.T) {
	r := newRegistry()
	reg(r, "app", &app{})
	reg(r, "workerb", &workerB{}, workerType) // needs storage
	reg(r, "storea", &storeA{}, storageType)
	root := r.Virtual("core", &virtualRoot{}, &fail.Collector{})
	res := mustResolveRoot(t, r, root, Controls{})
	sub, ok := res.Subtree("workerb")
	if !ok {
		t.Fatal("workerb is a closure member")
	}
	if len(sub.Ordered) != 2 || sub.Ordered[0].Desc.ID != "storea" || sub.Ordered[1].Desc.ID != "workerb" {
		t.Errorf("subtree must be the reachable set in dependency order: %v", ids(sub))
	}
	if _, ok := res.Subtree("ghost"); ok {
		t.Error("a non-member must report ok=false")
	}
}
