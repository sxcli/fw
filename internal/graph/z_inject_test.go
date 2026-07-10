package graph

import (
	"testing"

	"sxcli.dev/fw/internal/fail"
	"sxcli.dev/fw/internal/registry"
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
	r.Register("app", theApp, registry.Options{})
	r.Register("workerb", wb, provides(workerType))
	r.Register("storea", sa, provides(storageType))
	mustInject(t, mustResolve(t, r, "app", nil, Controls{}))
	if theApp.W != worker(wb) {
		t.Errorf("interface field not wired: %v", theApp.W)
	}
	if wb.S != storage(sa) {
		t.Errorf("transitive dependency not wired: %v", wb.S)
	}
	store := &appStore{}
	r2 := newRegistry()
	r2.Register("appstore", store, registry.Options{})
	r2.Register("storea", sa, provides(storageType))
	mustInject(t, mustResolve(t, r2, "appstore", nil, Controls{}))
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
	r.Register("appall", theApp, registry.Options{})
	r.Register("workera", wa, provides(workerType))
	r.Register("workerb", wb, provides(workerType))
	r.Register("storea", sa, provides(storageType))
	mustInject(t, mustResolve(t, r, "appall", nil, Controls{}))
	if len(theApp.Ws) != 2 || theApp.Ws[0] != worker(wa) || theApp.Ws[1] != worker(wb) {
		t.Errorf("slice not wired in registration order: %v", theApp.Ws)
	}
}

func TestInjectLeavesUnmatchedOptionalUntouched(t *testing.T) {
	r := newRegistry()
	theApp := &appOptional{}
	r.Register("appopt", theApp, registry.Options{})
	mustInject(t, mustResolve(t, r, "appopt", nil, Controls{}))
	if theApp.W != nil {
		t.Errorf("unmatched optional field must stay nil: %v", theApp.W)
	}
}

func TestInjectWiresCycleBothWays(t *testing.T) {
	r := newRegistry()
	p1 := &ping{}
	p2 := &pong{}
	r.Register("ping", p1, provides(workerType))
	r.Register("pong", p2, provides(storageType))
	mustInject(t, mustResolve(t, r, "ping", nil, Controls{}))
	if p1.Peer != storage(p2) || p2.Peer != worker(p1) {
		t.Errorf("cycle members not mutually wired: %v, %v", p1.Peer, p2.Peer)
	}
}

func TestInjectReportsNilEmbeddedPointer(t *testing.T) {
	r := newRegistry()
	r.Register("derived", &derived{}, registry.Options{}) // base is nil
	r.Register("workera", &workerA{}, provides(workerType))
	res := mustResolve(t, r, "derived", nil, Controls{})
	c := &fail.Collector{}
	res.Inject(c)
	if c.Len() != 1 {
		t.Errorf("expected one nil-embedded-pointer error, got %v", c.All())
	}
}
