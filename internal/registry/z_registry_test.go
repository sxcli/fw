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

// Ledger note: this file once exercised the Register method — id shape,
// instance shape, Provides and config validation, semantic checks. All
// of that moved into the root package's typed registration chain
// (z_catalog_test.go pins it); the registry now owns id uniqueness,
// inject-tag collection, retention and the debug dump, tested here.
package registry

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"sxcli.dev/conf/fail"
)

func newReg() (*Registry, *fail.Collector) {
	c := &fail.Collector{}
	return New(c), c
}

// commit stores instance under id the way the root's chain does: the
// descriptor arrives with identity and shape already validated.
func commit(r *Registry, id string, instance any) *Descriptor {
	d := &Descriptor{ID: id, Instance: instance, Concrete: reflect.TypeOf(instance), Aliases: []string{id}}
	r.Commit(d)
	return d
}

type greeter interface {
	Greet() string
}

type svcA struct{}

func (s *svcA) Greet() string { return "a" }

type svcB struct {
	Single greeter   `inject:""`
	All    []greeter `inject:""`
	Named  greeter   `inject:"svca"`
	Opt    greeter   `inject:";optional"`
	Ptr    *svcA     `inject:""`
}

func (s *svcB) Greet() string { return "b" }

type svcUnexported struct {
	dep greeter `inject:""` //nolint:unused
}

type svcSlicePtr struct {
	Deps []*svcA `inject:""`
}

type svcBadFlag struct {
	Dep greeter `inject:";maybe"`
}

type svcMultiIDSingle struct {
	Dep greeter `inject:"a,b"`
}

type svcBadKind struct {
	Dep int `inject:""`
}

type svcPlain struct{}

var greeterType = reflect.TypeOf((*greeter)(nil)).Elem()

func TestCommitHappyPath(t *testing.T) {
	r, c := newReg()
	commit(r, "svca", &svcA{})
	commit(r, "svcb", &svcB{})
	if c.Len() != 0 {
		t.Fatalf("unexpected errors: %v", c.All())
	}
	if len(r.All()) != 2 {
		t.Fatalf("expected 2 descriptors, got %d", len(r.All()))
	}
	if r.All()[0].ID != "svca" || r.All()[1].ID != "svcb" {
		t.Errorf("commit order not preserved: %q, %q", r.All()[0].ID, r.All()[1].ID)
	}
	d, ok := r.ByID("svcb")
	if !ok {
		t.Fatal("svcb not found by id")
	}
	if len(d.Deps) != 5 {
		t.Fatalf("expected 5 dep fields, got %d: %+v", len(d.Deps), d.Deps)
	}
	want := []DepField{
		{Index: []int{0}, Name: "Single", Type: greeterType, IsSlice: false, IDs: nil, Optional: false},
		{Index: []int{1}, Name: "All", Type: greeterType, IsSlice: true, IDs: nil, Optional: false},
		{Index: []int{2}, Name: "Named", Type: greeterType, IsSlice: false, IDs: []string{"svca"}, Optional: false},
		{Index: []int{3}, Name: "Opt", Type: greeterType, IsSlice: false, IDs: nil, Optional: true},
		{Index: []int{4}, Name: "Ptr", Type: reflect.TypeOf(&svcA{}), IsSlice: false, IDs: nil, Optional: false},
	}
	for i, w := range want {
		if !reflect.DeepEqual(d.Deps[i], w) {
			t.Errorf("dep %d: got %+v, want %+v", i, d.Deps[i], w)
		}
	}
}

func TestCommitRejectsDuplicateID(t *testing.T) {
	r, c := newReg()
	commit(r, "svca", &svcA{})
	commit(r, "svca", &svcPlain{})
	if c.Len() == 0 {
		t.Error("duplicate id must be recorded")
	}
	if len(r.All()) != 1 {
		t.Errorf("expected 1 stored descriptor, got %d", len(r.All()))
	}
}

// The same concrete type MAY be cataloged twice; only accepting both
// into one composition is a violation, and that is Build's check.
func TestCommitToleratesDuplicateConcreteType(t *testing.T) {
	r, c := newReg()
	commit(r, "one", &svcA{})
	commit(r, "two", &svcA{})
	if c.Len() != 0 {
		t.Errorf("unexpected errors: %v", c.All())
	}
	if len(r.All()) != 2 {
		t.Errorf("expected 2 stored descriptors, got %d", len(r.All()))
	}
}

// Tag-structure violations are the registry's own: collectDeps finds
// them at commit while the descriptor is still stored.
func TestCommitTagViolationsStillStore(t *testing.T) {
	cases := []struct {
		name     string
		instance any
	}{
		{"inject on unexported field", &svcUnexported{}},
		{"inject slice of concrete type", &svcSlicePtr{}},
		{"inject unknown flag", &svcBadFlag{}},
		{"inject multiple ids on single field", &svcMultiIDSingle{}},
		{"inject unsupported field kind", &svcBadKind{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, c := newReg()
			commit(r, "svc", tc.instance)
			if c.Len() == 0 {
				t.Error("expected a recorded error, got none")
			}
			if len(r.All()) != 1 {
				t.Errorf("descriptor should still be stored, got %d", len(r.All()))
			}
		})
	}
}

// Commit must tolerate re-collection: Build copies catalog descriptors
// and commits the copies into the composition's registry, so a second
// collectDeps over an already-collected descriptor must not double the
// dependency list.
func TestCommitIsIdempotentOverDeps(t *testing.T) {
	r, _ := newReg()
	d := commit(r, "svcb", &svcB{})
	r2, c2 := newReg()
	cp := *d
	r2.Commit(&cp)
	if c2.Len() != 0 {
		t.Fatalf("unexpected errors: %v", c2.All())
	}
	if len(cp.Deps) != 5 {
		t.Errorf("re-commit doubled the deps: %d", len(cp.Deps))
	}
}

func TestParseInjectTag(t *testing.T) {
	cases := []struct {
		tag      string
		ids      []string
		optional bool
		wantErr  bool
	}{
		{"", nil, false, false},
		{";optional", nil, true, false},
		{"a", []string{"a"}, false, false},
		{"a,b, c", []string{"a", "b", "c"}, false, false},
		{"a;optional", []string{"a"}, true, false},
		{"a,b;optional", []string{"a", "b"}, true, false},
		{";maybe", nil, false, true},
		{"a;optional;x", nil, false, true},
		{"A", nil, false, true},
		{"a,", nil, false, true},
		{",a", nil, false, true},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%q", tc.tag), func(t *testing.T) {
			ids, optional, err := parseInjectTag(tc.tag)
			if tc.wantErr != (err != nil) {
				t.Fatalf("error mismatch: got %v, wantErr=%v", err, tc.wantErr)
			}
			if err == nil {
				if !reflect.DeepEqual(ids, tc.ids) || optional != tc.optional {
					t.Errorf("got ids=%v optional=%v, want ids=%v optional=%v", ids, optional, tc.ids, tc.optional)
				}
			}
		})
	}
}

func TestRetainEjectsCold(t *testing.T) {
	r, _ := newReg()
	commit(r, "svca", &svcA{})
	commit(r, "svcb", &svcB{})
	commit(r, "plain", &svcPlain{})
	r.Retain(map[string]bool{"svca": true, "plain": true})
	if len(r.All()) != 2 {
		t.Fatalf("expected 2 retained descriptors, got %d", len(r.All()))
	}
	if _, ok := r.ByID("svcb"); ok {
		t.Error("ejected service still resolvable by id")
	}
	if r.All()[0].ID != "svca" || r.All()[1].ID != "plain" {
		t.Errorf("retain must preserve registration order: %q, %q", r.All()[0].ID, r.All()[1].ID)
	}
}

func TestDumpReadable(t *testing.T) {
	r, _ := newReg()
	cfg := &struct {
		Path  string
		Level int
	}{Path: "/var/log/app.log"}
	da := commit(r, "svca", &svcA{})
	da.Provides = []reflect.Type{greeterType}
	db := commit(r, "svcb", &svcB{})
	db.ConfigPtr = cfg
	commit(r, "plain", &svcPlain{})
	commit(r, "svca", &svcBadFlag{}) // duplicate id → recorded error
	var b strings.Builder
	r.Dump(&b)
	t.Logf("registry dump:\n%s", b.String())
}

func TestIsValidID(t *testing.T) {
	// the identity model (composition release) legalized path-shaped
	// ids: dots, hyphens and slashes are id characters now
	valid := []string{"a", "svca", "svc_a", "_svc", "s1", "svc-a", "svc.a", "example.com/x/svc"}
	invalid := []string{"", "A", "svcA", "1svc", "svc a", "-svc"}
	for _, id := range valid {
		if !isValidID(id) {
			t.Errorf("%q should be valid", id)
		}
	}
	for _, id := range invalid {
		if isValidID(id) {
			t.Errorf("%q should be invalid", id)
		}
	}
}
