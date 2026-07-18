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

// Ledger note: this file once tested the pre-composition Register API
// (options, instance registration). Everything it pinned lives on in
// z_catalog_test.go against the chain; what remains here are the
// shared fixture types and the applet-lifecycle table, ported.
package sxclifw

import (
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

func TestAppletLifecycleCheck(t *testing.T) {
	commit := func(reg func(reg *registry.Registry, c *fail.Collector)) int {
		r, c := catalogWorld()
		reg(r, c)
		return c.Len()
	}
	if n := commit(func(r *registry.Registry, c *fail.Collector) {
		NewBareRegistration("x/plain", func() *plainService { return &plainService{} }).
			Alias("plain").registerInto(r, c)
	}); n != 0 {
		t.Error("plain service must commit")
	}
	if n := commit(func(r *registry.Registry, c *fail.Collector) {
		NewBareRegistration("x/good", func() *goodApplet { return &goodApplet{} }).
			Alias("good").registerInto(r, c)
	}); n != 0 {
		t.Error("applet without lifecycle must commit")
	}
	if n := commit(func(r *registry.Registry, c *fail.Collector) {
		NewBareRegistration("x/stops", func() *lifecycleApplet { return &lifecycleApplet{} }).
			Alias("stops").registerInto(r, c)
	}); n == 0 {
		t.Error("applet with Stop must be rejected")
	}
	if n := commit(func(r *registry.Registry, c *fail.Collector) {
		NewBareRegistration("x/starts", func() *startingApplet { return &startingApplet{} }).
			Alias("starts").registerInto(r, c)
	}); n == 0 {
		t.Error("applet with Start and Stop must be rejected")
	}
}
