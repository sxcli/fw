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
)

type embedStore interface{ Probe() string }

// EmbedDepsExported is embedded exported; its inject field promotes
// into the collected deps.
type EmbedDepsExported struct {
	V embedStore `inject:""`
}

type embedPromoted struct {
	EmbedDepsExported
	N int
}

func (e *embedPromoted) Configured() error { return nil }
func (e *embedPromoted) Run() int          { return 0 }

// embedHidden buries an inject tag under an unexported embedded
// member — invisible per spec §4, refused at the catalog.
type embedHidden struct {
	H embedStore `inject:""`
}

type hiddenCarrier struct {
	embedHidden
	N int
}

func (h *hiddenCarrier) Configured() error { return nil }
func (h *hiddenCarrier) Run() int          { return 0 }

func TestPromotedInjectIsCollected(t *testing.T) {
	reg, c := catalogWorld()
	NewBareRegistration("embed/promoted", func() *embedPromoted { return &embedPromoted{} }).
		Alias("promoted").registerInto(reg, c)
	if c.Len() != 0 {
		t.Fatalf("a promoted exported dep must register clean: %v", c.All())
	}
	d, _ := reg.ByID("embed/promoted")
	if len(d.Deps) != 1 || d.Deps[0].Name != "V" {
		t.Errorf("the promoted dep must be collected: %+v", d.Deps)
	}
}

func TestHiddenInjectIsViolation(t *testing.T) {
	reg, c := catalogWorld()
	NewBareRegistration("embed/hidden", func() *hiddenCarrier { return &hiddenCarrier{} }).
		Alias("hidden").registerInto(reg, c)
	want := `service "embed/hidden" field H: inject tag under the unexported embedded field embedHidden — an unexported member and everything under it is invisible`
	if c.Len() == 0 || !strings.Contains(c.All()[0].Error(), want) {
		t.Errorf("missing %q in %v", want, c.All())
	}
	if d, registered := reg.ByID("embed/hidden"); registered {
		for _, dep := range d.Deps {
			if dep.Name == "H" {
				t.Error("a hidden dep must never be collected")
			}
		}
	}
}
