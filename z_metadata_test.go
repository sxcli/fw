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
