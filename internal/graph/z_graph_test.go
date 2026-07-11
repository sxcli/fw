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

func provides(types ...reflect.Type) registry.Options {
	return registry.Options{Interfaces: types}
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

func mustResolve(t *testing.T, reg *registry.Registry, appletID string, alwaysOn []string, ctl Controls) Result {
	t.Helper()
	c := &fail.Collector{}
	res := Resolve(c, reg, appletID, alwaysOn, ctl)
	if c.Len() != 0 {
		t.Fatalf("unexpected resolve errors: %v", c.All())
	}
	return res
}

func TestChainOrderAndBindings(t *testing.T) {
	r := newRegistry()
	r.Register("app", &app{}, registry.Options{})
	r.Register("workerb", &workerB{}, provides(workerType))
	r.Register("storea", &storeA{}, provides(storageType))
	res := mustResolve(t, r, "app", nil, Controls{})
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
	r.Register("app", &app{}, registry.Options{})
	r.Register("workera", &workerA{}, provides(workerType))
	r.Register("storea", &storeA{}, provides(storageType)) // nothing pulls it
	res := mustResolve(t, r, "app", nil, Controls{})
	if len(res.Ordered) != 2 {
		t.Errorf("cold service leaked into closure: %v", ids(res))
	}
}

func TestFirstRegisteredWins(t *testing.T) {
	r := newRegistry()
	r.Register("app", &app{}, registry.Options{})
	r.Register("workera", &workerA{}, provides(workerType))
	r.Register("workerb", &workerB{}, provides(workerType))
	r.Register("storea", &storeA{}, provides(storageType))
	res := mustResolve(t, r, "app", nil, Controls{})
	m := res.Ordered[position(t, res, "app")]
	if m.Bindings[0].Targets[0].ID != "workera" {
		t.Errorf("expected first registered match, got %q", m.Bindings[0].Targets[0].ID)
	}
	if len(res.Ordered) != 2 {
		t.Errorf("only the picked candidate should join the closure: %v", ids(res))
	}
}

func TestSliceGathersLateJoiners(t *testing.T) {
	r := newRegistry()
	r.Register("appseeded", &appSeeded{}, registry.Options{})
	r.Register("workera", &workerA{}, provides(workerType))
	r.Register("workerb", &workerB{}, provides(workerType)) // joins via always-on, not via the seed list
	r.Register("storea", &storeA{}, provides(storageType))
	res := mustResolve(t, r, "appseeded", []string{"workerb"}, Controls{})
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
	r.Register("appall", &appAll{}, registry.Options{})
	r.Register("workera", &workerA{}, provides(workerType))
	r.Register("workerb", &workerB{}, provides(workerType))
	r.Register("storea", &storeA{}, provides(storageType))
	res := mustResolve(t, r, "appall", nil, Controls{})
	if len(res.Ordered) != 4 {
		t.Errorf("bare slice must pull every registered match: %v", ids(res))
	}
}

func TestOptionalMissingIsFine(t *testing.T) {
	r := newRegistry()
	r.Register("appopt", &appOptional{}, registry.Options{})
	res := mustResolve(t, r, "appopt", nil, Controls{})
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
			r.Register("app", &app{}, registry.Options{})
		}, "app", Controls{}},
		{"unknown id in tag", func(r *registry.Registry) {
			r.Register("appbyid", &appByID{}, registry.Options{})
		}, "appbyid", Controls{}},
		{"unknown applet", func(r *registry.Registry) {}, "ghost", Controls{}},
		{"disabled applet", func(r *registry.Registry) {
			r.Register("app", &app{}, registry.Options{})
			r.Register("workera", &workerA{}, provides(workerType))
		}, "app", Controls{Disable: []string{"app"}}},
		{"disabled required by-id dependency", func(r *registry.Registry) {
			r.Register("appbyid", &appByID{}, registry.Options{})
			r.Register("workerb", &workerB{}, provides(workerType))
			r.Register("storea", &storeA{}, provides(storageType))
		}, "appbyid", Controls{Disable: []string{"workerb"}}},
		{"disable unknown id", func(r *registry.Registry) {
			r.Register("app", &app{}, registry.Options{})
			r.Register("workera", &workerA{}, provides(workerType))
		}, "app", Controls{Disable: []string{"ghost"}}},
		{"enable unknown id", func(r *registry.Registry) {
			r.Register("app", &app{}, registry.Options{})
			r.Register("workera", &workerA{}, provides(workerType))
		}, "app", Controls{Enable: []string{"ghost"}}},
		{"enabled and disabled", func(r *registry.Registry) {
			r.Register("app", &app{}, registry.Options{})
			r.Register("workera", &workerA{}, provides(workerType))
		}, "app", Controls{Enable: []string{"workera"}, Disable: []string{"workera"}}},
		{"override to unknown substitute", func(r *registry.Registry) {
			r.Register("app", &app{}, registry.Options{})
			r.Register("workera", &workerA{}, provides(workerType))
		}, "app", Controls{Override: map[string]string{"workera": "ghost"}}},
		{"override type mismatch", func(r *registry.Registry) {
			r.Register("appbyid", &appByID{}, registry.Options{})
			r.Register("workerb", &workerB{}, provides(workerType))
			r.Register("storea", &storeA{}, provides(storageType))
		}, "appbyid", Controls{Override: map[string]string{"workerb": "storea"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRegistry()
			tc.setup(r)
			c := &fail.Collector{}
			Resolve(c, r, tc.applet, nil, tc.ctl)
			if c.Len() == 0 {
				t.Error("expected resolve errors, got none")
			}
		})
	}
}

func TestDisableSteersBareField(t *testing.T) {
	r := newRegistry()
	r.Register("app", &app{}, registry.Options{})
	r.Register("workera", &workerA{}, provides(workerType))
	r.Register("workerb", &workerB{}, provides(workerType))
	r.Register("storea", &storeA{}, provides(storageType))
	res := mustResolve(t, r, "app", nil, Controls{Disable: []string{"workera"}})
	m := res.Ordered[position(t, res, "app")]
	if m.Bindings[0].Targets[0].ID != "workerb" {
		t.Errorf("disable must steer to the next candidate, got %q", m.Bindings[0].Targets[0].ID)
	}
}

func TestOverrideSubstitutes(t *testing.T) {
	r := newRegistry()
	r.Register("appbyid", &appByID{}, registry.Options{})
	r.Register("workera", &workerA{}, provides(workerType))
	r.Register("workerb", &workerB{}, provides(workerType))
	r.Register("storea", &storeA{}, provides(storageType))
	res := mustResolve(t, r, "appbyid", nil, Controls{Disable: []string{"workerb"}, Override: map[string]string{"workerb": "workera"}})
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
	r.Register("app", &app{}, registry.Options{})
	r.Register("workera", &workerA{}, provides(workerType))
	res := mustResolve(t, r, "app", nil, Controls{Override: map[string]string{
		"ghost":   "workera", // unregistered key: legal rescue mapping, but unused here
		"unfired": "workera",
	}})
	if strings.Join(res.UnusedOverrides, ",") != "ghost,unfired" {
		t.Errorf("unused overrides must be reported sorted: %v", res.UnusedOverrides)
	}
}

func TestEnableForcesColdService(t *testing.T) {
	r := newRegistry()
	r.Register("app", &app{}, registry.Options{})
	r.Register("workera", &workerA{}, provides(workerType))
	r.Register("workerb", &workerB{}, provides(workerType)) // cold unless enabled; drags storea
	r.Register("storea", &storeA{}, provides(storageType))
	res := mustResolve(t, r, "app", nil, Controls{Enable: []string{"workerb"}})
	if len(res.Ordered) != 4 {
		t.Errorf("enable must pull the service and its deps: %v", ids(res))
	}
	if !(position(t, res, "storea") < position(t, res, "workerb")) {
		t.Errorf("enabled service must still start after its deps: %v", ids(res))
	}
}

func TestConcreteTypeDependency(t *testing.T) {
	r := newRegistry()
	r.Register("appstore", &appStore{}, registry.Options{})
	r.Register("storea", &storeA{}, provides(storageType))
	res := mustResolve(t, r, "appstore", nil, Controls{})
	m := res.Ordered[position(t, res, "appstore")]
	if m.Bindings[0].Targets[0].ID != "storea" {
		t.Errorf("concrete type dependency not resolved: %+v", m.Bindings)
	}
}

func TestCycleIsWarningNotError(t *testing.T) {
	r := newRegistry()
	r.Register("ping", &ping{}, provides(workerType))
	r.Register("pong", &pong{}, provides(storageType))
	res := mustResolve(t, r, "ping", nil, Controls{})
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
	r.Register("selfish", &selfish{}, provides(workerType))
	res := mustResolve(t, r, "selfish", nil, Controls{})
	if !reflect.DeepEqual(res.Cycles, [][]string{{"selfish"}}) {
		t.Errorf("self-loop not reported: %v", res.Cycles)
	}
}

func TestAlwaysOnJoinsAndDisabledAlwaysOnSkips(t *testing.T) {
	r := newRegistry()
	r.Register("app", &app{}, registry.Options{})
	r.Register("workera", &workerA{}, provides(workerType))
	r.Register("storea", &storeA{}, provides(storageType))
	res := mustResolve(t, r, "app", []string{"storea"}, Controls{})
	if len(res.Ordered) != 3 {
		t.Errorf("always-on must join the closure: %v", ids(res))
	}
	res = mustResolve(t, r, "app", []string{"storea"}, Controls{Disable: []string{"storea"}})
	if len(res.Ordered) != 2 {
		t.Errorf("disabled always-on must be skipped: %v", ids(res))
	}
}

func TestDiamondResolvesOnce(t *testing.T) {
	r := newRegistry()
	r.Register("appall", &appAll{}, registry.Options{})
	r.Register("ping", &ping{}, provides(workerType))
	r.Register("workerb", &workerB{}, provides(workerType)) // both need storage
	r.Register("storea", &storeA{}, provides(storageType))
	res := mustResolve(t, r, "appall", nil, Controls{})
	if len(res.Ordered) != 4 {
		t.Fatalf("diamond dependency duplicated or lost: %v", ids(res))
	}
	if !(position(t, res, "storea") < position(t, res, "ping") && position(t, res, "storea") < position(t, res, "workerb")) {
		t.Errorf("shared dependency must precede both dependents: %v", ids(res))
	}
}
