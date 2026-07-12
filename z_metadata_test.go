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
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func annotatedWorld(t *testing.T, md *Metadata) (*world, *extraService) {
	t.Helper()
	w := newWorld(t, []string{"bin", "meta"}, nil, nil)
	w.applet(0)
	extra := &extraService{cfg: extraCfg{Flag: "fast"}}
	w.rt.reg.Register("extra", extra, foldOptions([]RegisterOption{WithConfig(&extra.cfg), WithMetadata(md)}))
	return w, extra
}

func TestMetadataFlowsIntoIntrospection(t *testing.T) {
	md := &Metadata{
		Description: "an example service with a closed flag domain",
		Fields: map[string]any{
			"Flag": FieldMetadata[string]{Allowed: []string{"fast", "slow"}, Doc: "the pace of things"},
		},
	}
	var desc string
	var infos []ArgInfo
	w, _ := annotatedWorld(t, md)
	probe := &argsProbe{do: func(i *Introspector) {
		desc = i.Describe("extra")
		infos, _ = i.Arguments("app", []string{"--enable", "extra"})
	}}
	w.rt.reg.Register("meta", probe, foldOptions([]RegisterOption{}))
	if code := run(w.rt); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
	if desc != "an example service with a closed flag domain" {
		t.Errorf("Describe wrong: %q", desc)
	}
	found := false
	for _, a := range infos {
		if a.Long == "extra-flag" {
			found = true
			if fmt.Sprint(a.Allowed) != "[fast slow]" || a.Doc != "the pace of things" {
				t.Errorf("annotation not exposed: %+v", a)
			}
		}
	}
	if !found {
		t.Fatalf("extra-flag not in schema: %v", infos)
	}
}

func TestMetadataViolations(t *testing.T) {
	cases := []struct {
		name string
		md   *Metadata
		want string
	}{
		{"unknown field", &Metadata{Fields: map[string]any{"Nope": FieldMetadata[string]{}}}, "names no config field"},
		{"wrong value type", &Metadata{Fields: map[string]any{"Flag": "not field metadata"}}, "must be a FieldMetadata"},
		{"type mismatch", &Metadata{Fields: map[string]any{"Flag": FieldMetadata[int]{Allowed: []int{1, 2}}}}, "allows int values"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := annotatedWorld(t, tc.md)
			if w.c.Len() == 0 {
				t.Fatal("expected a registration violation")
			}
			joined := ""
			for _, err := range w.c.All() {
				joined += err.Error() + "\n"
			}
			if !strings.Contains(joined, tc.want) {
				t.Errorf("want %q in:\n%s", tc.want, joined)
			}
		})
	}
}

func TestMetadataFieldsWithoutConfigIsViolation(t *testing.T) {
	w := newWorld(t, []string{"bin"}, nil, nil)
	w.applet(0)
	w.rt.reg.Register("bare", &plainService{}, foldOptions([]RegisterOption{
		WithMetadata(&Metadata{Fields: map[string]any{"X": FieldMetadata[string]{}}}),
	}))
	if w.c.Len() == 0 {
		t.Fatal("field metadata without a config struct must be a violation")
	}
}

func TestDescriptionAloneWithoutConfigIsFine(t *testing.T) {
	w := newWorld(t, []string{"bin"}, nil, nil)
	w.applet(0)
	w.rt.reg.Register("bare", &plainService{}, foldOptions([]RegisterOption{
		WithMetadata(&Metadata{Description: "a config-less but well-described service"}),
	}))
	if w.c.Len() != 0 {
		t.Fatalf("description-only metadata must be fine: %v", w.c.All())
	}
}

func enforcementWorld(t *testing.T, argv []string, files, env map[string]string) (*world, *extraService) {
	t.Helper()
	w := newWorld(t, argv, files, env)
	w.applet(0)
	extra := &extraService{cfg: extraCfg{Flag: "fast"}}
	w.rt.reg.Register("extra", extra, foldOptions([]RegisterOption{
		WithConfig(&extra.cfg),
		WithMetadata(&Metadata{Fields: map[string]any{
			"Flag": FieldMetadata[string]{Allowed: []string{"fast", "slow"}},
			"Tags": FieldMetadata[string]{Allowed: []string{"a", "b"}},
		}}),
	}))
	return w, extra
}

func TestDomainEnforcedOnArguments(t *testing.T) {
	w, _ := enforcementWorld(t, []string{"bin", "--enable", "extra", "--extra-flag", "turbo"}, nil, nil)
	if code := run(w.rt); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(w.stderr.String(), "not among the allowed values") {
		t.Errorf("domain violation not reported:\n%s", w.stderr.String())
	}
	w2, extra := enforcementWorld(t, []string{"bin", "--enable", "extra", "--extra-flag", "slow"}, nil, nil)
	if code := run(w2.rt); code != 0 {
		t.Fatalf("in-domain value must pass: exit %d\n%s", code, w2.stderr.String())
	}
	if extra.cfg.Flag != "slow" {
		t.Errorf("value not applied: %q", extra.cfg.Flag)
	}
}

func TestDomainEnforcedOnEnvironment(t *testing.T) {
	w, _ := enforcementWorld(t, []string{"bin", "--enable", "extra"}, nil, map[string]string{"APP_EXTRA_FLAG": "turbo"})
	if code := run(w.rt); code != 2 {
		t.Fatalf("exit = %d, want 2\n%s", code, w.stderr.String())
	}
	if !strings.Contains(w.stderr.String(), "$APP_EXTRA_FLAG") {
		t.Errorf("violation must name the env source:\n%s", w.stderr.String())
	}
}

func TestDomainEnforcedOnFiles(t *testing.T) {
	files := map[string]string{"/etc/app/config.json": `{"extra": {"flag": "turbo"}}`}
	w, _ := enforcementWorld(t, []string{"bin", "--enable", "extra"}, files, nil)
	if code := run(w.rt); code != 2 {
		t.Fatalf("exit = %d, want 2\n%s", code, w.stderr.String())
	}
	if !strings.Contains(w.stderr.String(), "config extra.flag") {
		t.Errorf("violation must name the file source:\n%s", w.stderr.String())
	}
}

func TestSliceDomainEnforced(t *testing.T) {
	w, _ := enforcementWorld(t, []string{"bin", "--enable", "extra", "--extra-tag", "a", "--extra-tag", "z"}, nil, nil)
	if code := run(w.rt); code != 2 {
		t.Fatalf("bad slice element must fail: exit %d", code)
	}
	w2, extra := enforcementWorld(t, []string{"bin", "--enable", "extra", "--extra-tag", "a", "--extra-tag", "b"}, nil, nil)
	if code := run(w2.rt); code != 0 {
		t.Fatalf("in-domain elements must pass: exit %d\n%s", code, w2.stderr.String())
	}
	if strings.Join(extra.cfg.Tags, ",") != "a,b" {
		t.Errorf("values not applied: %v", extra.cfg.Tags)
	}
}

func TestDefaultOutsideDomainIsRegistrationViolation(t *testing.T) {
	w := newWorld(t, []string{"bin"}, nil, nil)
	w.applet(0)
	extra := &extraService{cfg: extraCfg{Flag: "turbo"}} // default not in the domain
	w.rt.reg.Register("extra", extra, foldOptions([]RegisterOption{
		WithConfig(&extra.cfg),
		WithMetadata(&Metadata{Fields: map[string]any{
			"Flag": FieldMetadata[string]{Allowed: []string{"fast", "slow"}},
		}}),
	}))
	if w.c.Len() == 0 {
		t.Fatal("a default outside its own declared domain must be a registration violation")
	}
}

func TestSliceDomainEnforcedFromFiles(t *testing.T) {
	files := map[string]string{"/etc/app/config.json": `{"extra": {"tags": ["a", "z"]}}`}
	w, _ := enforcementWorld(t, []string{"bin", "--enable", "extra"}, files, nil)
	if code := run(w.rt); code != 2 {
		t.Fatalf("exit = %d, want 2\n%s", code, w.stderr.String())
	}
	if !strings.Contains(w.stderr.String(), "config extra.tags") {
		t.Errorf("violation must name the file source:\n%s", w.stderr.String())
	}
}

func TestSliceDomainEnforcedFromEnvironment(t *testing.T) {
	w, _ := enforcementWorld(t, []string{"bin", "--enable", "extra"}, nil, map[string]string{"APP_EXTRA_TAG": "a,z"})
	if code := run(w.rt); code != 2 {
		t.Fatalf("exit = %d, want 2\n%s", code, w.stderr.String())
	}
	if !strings.Contains(w.stderr.String(), "$APP_EXTRA_TAG") {
		t.Errorf("violation must name the env source:\n%s", w.stderr.String())
	}
}

func TestSliceDefaultOutsideDomainIsRegistrationViolation(t *testing.T) {
	w := newWorld(t, []string{"bin"}, nil, nil)
	w.applet(0)
	extra := &extraService{cfg: extraCfg{Flag: "fast", Tags: []string{"a", "zz"}}}
	w.rt.reg.Register("extra", extra, foldOptions([]RegisterOption{
		WithConfig(&extra.cfg),
		WithMetadata(&Metadata{Fields: map[string]any{
			"Flag": FieldMetadata[string]{Allowed: []string{"fast", "slow"}},
			"Tags": FieldMetadata[string]{Allowed: []string{"a", "b"}},
		}}),
	}))
	if w.c.Len() == 0 {
		t.Fatal("a default slice element outside the domain must be a registration violation")
	}
	found := false
	for _, err := range w.c.All() {
		found = found || strings.Contains(err.Error(), "default element")
	}
	if !found {
		t.Errorf("violation must name the offending default element: %v", w.c.All())
	}
}

func TestNilAndAbsentMetadataAreHarmless(t *testing.T) {
	// regression: registerOptions.metadata is a typed-nil *Metadata for
	// every metadata-less registration; the check must not treat it as
	// present (this panicked once, in the yaml provider's init)
	w := newWorld(t, []string{"bin"}, nil, nil)
	w.applet(0)
	plain := &extraService{cfg: extraCfg{Flag: "fast"}}
	w.rt.reg.Register("plainmeta", plain, foldOptions([]RegisterOption{WithConfig(&plain.cfg), WithMetadata(nil)}))
	if w.c.Len() != 0 {
		t.Fatalf("nil metadata must be treated as absent: %v", w.c.All())
	}
	if code := run(w.rt); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
}

func TestDescribeEdgeCases(t *testing.T) {
	var unknown, unannotated string
	w := newWorld(t, []string{"bin", "meta"}, nil, nil)
	w.applet(0)
	w.dep(false) // registered, no metadata
	probe := &argsProbe{do: func(i *Introspector) {
		unknown = i.Describe("nope")
		unannotated = i.Describe("dep")
	}}
	w.rt.reg.Register("meta", probe, foldOptions([]RegisterOption{}))
	if code := run(w.rt); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
	if unknown != "" || unannotated != "" {
		t.Errorf("Describe must return empty for unknown/unannotated: %q, %q", unknown, unannotated)
	}
}

// intService proves non-string domains end to end.
type intCfg struct {
	Retries int `json:"retries" arg:"retries-x" usage:"attempts"`
}

type intService struct {
	cfg intCfg
}

func intWorld(t *testing.T, argv []string) (*world, *intService) {
	t.Helper()
	w := newWorld(t, argv, nil, nil)
	w.applet(0)
	svc := &intService{cfg: intCfg{Retries: 1}}
	w.rt.reg.Register("intsvc", svc, foldOptions([]RegisterOption{
		WithConfig(&svc.cfg),
		WithMetadata(&Metadata{Fields: map[string]any{
			"Retries": FieldMetadata[int]{Allowed: []int{1, 3, 5}},
		}}),
	}))
	return w, svc
}

func TestIntDomainEnforcedEndToEnd(t *testing.T) {
	w, _ := intWorld(t, []string{"bin", "--enable", "intsvc", "--retries-x", "7"})
	if code := run(w.rt); code != 2 {
		t.Fatalf("out-of-domain int must fail: exit %d\n%s", code, w.stderr.String())
	}
	w2, svc := intWorld(t, []string{"bin", "--enable", "intsvc", "--retries-x", "3"})
	if code := run(w2.rt); code != 0 {
		t.Fatalf("in-domain int must pass: exit %d\n%s", code, w2.stderr.String())
	}
	if svc.cfg.Retries != 3 {
		t.Errorf("value not applied: %d", svc.cfg.Retries)
	}
}

func TestArgInfoSliceTypeIsElementType(t *testing.T) {
	var tagInfo *ArgInfo
	w, _ := enforcementWorld(t, []string{"bin", "meta"}, nil, nil)
	probe := &argsProbe{do: func(i *Introspector) {
		infos, _ := i.Arguments("app", []string{"--enable", "extra"})
		for idx := range infos {
			if infos[idx].Long == "extra-tag" {
				tagInfo = &infos[idx]
			}
		}
	}}
	w.rt.reg.Register("meta", probe, foldOptions([]RegisterOption{}))
	if code := run(w.rt); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
	if tagInfo == nil {
		t.Fatal("extra-tag not found in schema")
	}
	if !tagInfo.IsSlice || tagInfo.Type != reflect.TypeOf("") {
		t.Errorf("slice ArgInfo must carry the element type: IsSlice=%v Type=%v", tagInfo.IsSlice, tagInfo.Type)
	}
	if fmt.Sprint(tagInfo.Allowed) != "[a b]" {
		t.Errorf("slice domain must be exposed: %v", tagInfo.Allowed)
	}
}

func TestNamedTypeAllowedValuesConvert(t *testing.T) {
	type pace string
	// []pace on a string field: same kind, convertible — legal
	w, _ := annotatedWorld(t, &Metadata{Fields: map[string]any{
		"Flag": FieldMetadata[pace]{Allowed: []pace{"fast", "slow"}},
	}})
	if w.c.Len() != 0 {
		t.Fatalf("same-kind convertible allowed values must be accepted: %v", w.c.All())
	}
}
