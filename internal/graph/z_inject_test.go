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
	"testing"

	"sxcli.dev/conf/fail"
)

type base struct {
	W worker `inject:""`
}

type derived struct {
	*base
}

func (d *derived) Work() {}

func mustInject(t *testing.T, res Result) {
	t.Helper()
	c := &fail.Collector{}
	res.Inject(c)
	if c.Len() != 0 {
		t.Fatalf("unexpected inject errors: %v", c.All())
	}
}

func TestInjectWiresInterfaceAndConcreteFields(t *testing.T) {
	r := newRegistry()
	theApp := &app{}
	wb := &workerB{}
	sa := &storeA{}
	reg(r, "t/app", theApp)
	reg(r, "t/workerb", wb, workerType)
	reg(r, "t/storea", sa, storageType)
	mustInject(t, mustResolve(t, r, "t/app", Controls{}))
	if theApp.W != worker(wb) {
		t.Errorf("interface field not wired: %v", theApp.W)
	}
	if wb.S != storage(sa) {
		t.Errorf("transitive dependency not wired: %v", wb.S)
	}
	store := &appStore{}
	r2 := newRegistry()
	reg(r2, "t/appstore", store)
	reg(r2, "t/storea", sa, storageType)
	mustInject(t, mustResolve(t, r2, "t/appstore", Controls{}))
	if store.S != sa {
		t.Errorf("concrete field not wired: %v", store.S)
	}
}

func TestInjectFillsSliceInOrder(t *testing.T) {
	r := newRegistry()
	theApp := &appAll{}
	wa := &workerA{}
	wb := &workerB{}
	sa := &storeA{}
	reg(r, "t/appall", theApp)
	reg(r, "t/workera", wa, workerType)
	reg(r, "t/workerb", wb, workerType)
	reg(r, "t/storea", sa, storageType)
	mustInject(t, mustResolve(t, r, "t/appall", Controls{}))
	if len(theApp.Ws) != 2 || theApp.Ws[0] != worker(wa) || theApp.Ws[1] != worker(wb) {
		t.Errorf("slice not wired in registration order: %v", theApp.Ws)
	}
}

func TestInjectLeavesUnmatchedOptionalUntouched(t *testing.T) {
	r := newRegistry()
	theApp := &appOptional{}
	reg(r, "t/appopt", theApp)
	mustInject(t, mustResolve(t, r, "t/appopt", Controls{}))
	if theApp.W != nil {
		t.Errorf("unmatched optional field must stay nil: %v", theApp.W)
	}
}

func TestInjectWiresCycleBothWays(t *testing.T) {
	r := newRegistry()
	p1 := &ping{}
	p2 := &pong{}
	reg(r, "t/ping", p1, workerType)
	reg(r, "t/pong", p2, storageType)
	mustInject(t, mustResolve(t, r, "t/ping", Controls{}))
	if p1.Peer != storage(p2) || p2.Peer != worker(p1) {
		t.Errorf("cycle members not mutually wired: %v, %v", p1.Peer, p2.Peer)
	}
}

func TestInjectReportsNilEmbeddedPointer(t *testing.T) {
	r := newRegistry()
	reg(r, "t/derived", &derived{}) // base is nil
	reg(r, "t/workera", &workerA{}, workerType)
	res := mustResolve(t, r, "t/derived", Controls{})
	c := &fail.Collector{}
	res.Inject(c)
	if c.Len() != 1 {
		t.Errorf("expected one nil-embedded-pointer error, got %v", c.All())
	}
}
