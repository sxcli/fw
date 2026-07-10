package registry

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"sxcli.dev/fw/internal/fail"
)

func newReg(checks ...Check) (*Registry, *fail.Collector) {
	c := &fail.Collector{}
	return New(c, checks...), c
}

type greeter interface {
	Greet() string
}

type farewell interface {
	Bye() string
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
var farewellType = reflect.TypeOf((*farewell)(nil)).Elem()

func TestRegisterHappyPath(t *testing.T) {
	r, c := newReg()
	cfg := &struct{ N int }{N: 42}
	r.Register("svca", &svcA{}, Options{Interfaces: []reflect.Type{greeterType}})
	r.Register("svcb", &svcB{}, Options{Interfaces: []reflect.Type{greeterType}, Config: cfg})
	if c.Len() != 0 {
		t.Fatalf("unexpected errors: %v", c.All())
	}
	if len(r.All()) != 2 {
		t.Fatalf("expected 2 descriptors, got %d", len(r.All()))
	}
	if r.All()[0].ID != "svca" || r.All()[1].ID != "svcb" {
		t.Errorf("registration order not preserved: %q, %q", r.All()[0].ID, r.All()[1].ID)
	}
	d, ok := r.ByID("svcb")
	if !ok {
		t.Fatal("svcb not found by id")
	}
	if d.ConfigPtr != cfg {
		t.Error("config pointer not stored")
	}
	if len(d.Provides) != 1 || d.Provides[0] != greeterType {
		t.Errorf("provides not recorded: %v", d.Provides)
	}
	if d.Concrete != reflect.TypeOf(&svcB{}) {
		t.Errorf("concrete type wrong: %v", d.Concrete)
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

func TestRegisterIdentityViolations(t *testing.T) {
	cases := []struct {
		name     string
		register func(r *Registry)
		stored   int
	}{
		{"invalid id uppercase", func(r *Registry) { r.Register("SvcA", &svcA{}, Options{}) }, 0},
		{"invalid id empty", func(r *Registry) { r.Register("", &svcA{}, Options{}) }, 0},
		{"invalid id digit start", func(r *Registry) { r.Register("1svc", &svcA{}, Options{}) }, 0},
		{"invalid id dash", func(r *Registry) { r.Register("svc-a", &svcA{}, Options{}) }, 0},
		{"invalid id blank identifier", func(r *Registry) { r.Register("_", &svcA{}, Options{}) }, 0},
		{"duplicate id", func(r *Registry) {
			r.Register("svca", &svcA{}, Options{})
			r.Register("svca", &svcPlain{}, Options{})
		}, 1},
		{"duplicate concrete type", func(r *Registry) {
			r.Register("one", &svcA{}, Options{})
			r.Register("two", &svcA{}, Options{})
		}, 1},
		{"nil instance", func(r *Registry) { r.Register("svca", nil, Options{}) }, 0},
		{"typed nil pointer", func(r *Registry) { r.Register("svca", (*svcA)(nil), Options{}) }, 0},
		{"non-pointer instance", func(r *Registry) { r.Register("svca", svcA{}, Options{}) }, 0},
		{"pointer to non-struct", func(r *Registry) { i := 5; r.Register("svca", &i, Options{}) }, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, c := newReg()
			tc.register(r)
			if c.Len() == 0 {
				t.Error("expected a recorded error, got none")
			}
			if len(r.All()) != tc.stored {
				t.Errorf("expected %d stored descriptors, got %d", tc.stored, len(r.All()))
			}
		})
	}
}

func TestRegisterNonIdentityViolationsStillStore(t *testing.T) {
	cases := []struct {
		name     string
		register func(r *Registry)
	}{
		{"provides non-interface", func(r *Registry) {
			r.Register("svca", &svcA{}, Options{Interfaces: []reflect.Type{reflect.TypeOf(svcA{})}})
		}},
		{"provides unimplemented interface", func(r *Registry) {
			r.Register("svca", &svcA{}, Options{Interfaces: []reflect.Type{farewellType}})
		}},
		{"config not a pointer", func(r *Registry) {
			r.Register("svca", &svcA{}, Options{Config: struct{}{}})
		}},
		{"config typed nil pointer", func(r *Registry) {
			r.Register("svca", &svcA{}, Options{Config: (*struct{ N int })(nil)})
		}},
		{"inject on unexported field", func(r *Registry) {
			r.Register("svcu", &svcUnexported{}, Options{})
		}},
		{"inject slice of concrete type", func(r *Registry) {
			r.Register("svcs", &svcSlicePtr{}, Options{})
		}},
		{"inject unknown flag", func(r *Registry) {
			r.Register("svcf", &svcBadFlag{}, Options{})
		}},
		{"inject multiple ids on single field", func(r *Registry) {
			r.Register("svcm", &svcMultiIDSingle{}, Options{})
		}},
		{"inject unsupported field kind", func(r *Registry) {
			r.Register("svck", &svcBadKind{}, Options{})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, c := newReg()
			tc.register(r)
			if c.Len() == 0 {
				t.Error("expected a recorded error, got none")
			}
			if len(r.All()) != 1 {
				t.Errorf("descriptor should still be stored, got %d", len(r.All()))
			}
		})
	}
}

func TestChecksRunAndRecord(t *testing.T) {
	var seen []string
	failing := func(d *Descriptor) error {
		seen = append(seen, d.ID)
		return fmt.Errorf("check rejects %q", d.ID)
	}
	passing := func(d *Descriptor) error { return nil }
	r, c := newReg(failing, passing)
	r.Register("svca", &svcA{}, Options{})
	if len(seen) != 1 || seen[0] != "svca" {
		t.Errorf("check not invoked with descriptor: %v", seen)
	}
	if c.Len() != 1 {
		t.Errorf("expected exactly the failing check's error, got %v", c.All())
	}
	if len(r.All()) != 1 {
		t.Errorf("check failures must not discard the descriptor, got %d stored", len(r.All()))
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
	r.Register("svca", &svcA{}, Options{Interfaces: []reflect.Type{greeterType}})
	r.Register("svcb", &svcB{}, Options{Interfaces: []reflect.Type{greeterType}})
	r.Register("plain", &svcPlain{}, Options{})
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
	r.Register("svca", &svcA{}, Options{Interfaces: []reflect.Type{greeterType}})
	r.Register("svcb", &svcB{}, Options{Interfaces: []reflect.Type{greeterType}, Config: cfg})
	r.Register("plain", &svcPlain{}, Options{})
	r.Register("svca", &svcBadFlag{}, Options{}) // duplicate id → recorded error
	var b strings.Builder
	r.Dump(&b)
	t.Logf("registry dump:\n%s", b.String())
}

func TestIsValidID(t *testing.T) {
	valid := []string{"a", "svca", "svc_a", "_svc", "s1"}
	invalid := []string{"", "A", "svcA", "1svc", "svc-a", "svc a", "svc.a"}
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
