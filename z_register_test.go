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
	"reflect"
	"testing"

	"sxcli.dev/fw/internal/fail"
	"sxcli.dev/fw/internal/registry"
)

type pingService interface {
	Ping() string
}

type plainService struct{}

func (s *plainService) Ping() string { return "pong" }

type goodApplet struct{}

func (a *goodApplet) Configured() error { return nil }
func (a *goodApplet) Run() int          { return 0 }

type lifecycleApplet struct{}

func (a *lifecycleApplet) Configured() error { return nil }
func (a *lifecycleApplet) Run() int          { return 0 }
func (a *lifecycleApplet) Stop() error       { return nil }

type startingApplet struct{}

func (a *startingApplet) Configured() error { return nil }
func (a *startingApplet) Run() int          { return 0 }
func (a *startingApplet) Stop() error       { return nil }
func (a *startingApplet) Start() error      { return nil }

func newRootRegistry() (*registry.Registry, *fail.Collector) {
	c := &fail.Collector{}
	return registry.New(c, checkReservedID, checkAppletLifecycle), c
}

func fold(opts ...RegisterOption) registry.Options {
	var o registerOptions
	for _, opt := range opts {
		opt(&o)
	}
	return registry.Options{Interfaces: o.interfaces, Config: o.config}
}

func TestProvidesCapturesInterfaceType(t *testing.T) {
	o := fold(Provides[pingService](), Provides[Stopper]())
	want := []reflect.Type{
		reflect.TypeOf((*pingService)(nil)).Elem(),
		reflect.TypeOf((*Stopper)(nil)).Elem(),
	}
	if !reflect.DeepEqual(o.Interfaces, want) {
		t.Errorf("got %v, want %v", o.Interfaces, want)
	}
}

func TestProvidesNonInterfaceIsRecorded(t *testing.T) {
	r, c := newRootRegistry()
	r.Register("plain", &plainService{}, fold(Provides[plainService]()))
	if c.Len() == 0 {
		t.Error("Provides of a non-interface type must record an error")
	}
}

func TestWithConfigCapturesPointer(t *testing.T) {
	cfg := &struct{ N int }{N: 1}
	o := fold(WithConfig(cfg))
	if o.Config != cfg {
		t.Errorf("got %v, want %v", o.Config, cfg)
	}
}

func TestReservedCoreID(t *testing.T) {
	r, c := newRootRegistry()
	r.Register("core", &plainService{}, fold())
	if c.Len() == 0 {
		t.Error("registering under the reserved id must record an error")
	}
}

func TestAppletLifecycleCheck(t *testing.T) {
	cases := []struct {
		name     string
		instance any
		wantErr  bool
	}{
		{"plain service with lifecycle is fine", &plainService{}, false},
		{"applet without lifecycle is fine", &goodApplet{}, false},
		{"applet with Stop is rejected", &lifecycleApplet{}, true},
		{"applet with Start and Stop is rejected", &startingApplet{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, c := newRootRegistry()
			r.Register("subject", tc.instance, fold())
			if tc.wantErr != (c.Len() > 0) {
				t.Errorf("errors=%v, wantErr=%v", c.All(), tc.wantErr)
			}
		})
	}
}

func TestRegisterEndToEnd(t *testing.T) {
	r, c := newRootRegistry()
	cfg := &struct{ Level string }{Level: "info"}
	r.Register("plain", &plainService{}, fold(Provides[pingService](), WithConfig(cfg)))
	r.Register("app", &goodApplet{}, fold())
	if c.Len() != 0 {
		t.Fatalf("unexpected errors: %v", c.All())
	}
	d, ok := r.ByID("plain")
	if !ok {
		t.Fatal("plain not stored")
	}
	if d.ConfigPtr != cfg || len(d.Provides) != 1 {
		t.Errorf("descriptor incomplete: %+v", d)
	}
}
