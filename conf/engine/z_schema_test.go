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

package engine

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"sxcli.dev/fw/internal/fail"
)

type sinkConfig struct {
	Version  uint32        `json:"version"`
	Path     string        `json:"path" conf:"log-path" usage:"log file location"`
	Level    string        `json:"level" conf:"log-level,l" env:"LOG_LEVEL" usage:"minimum level"`
	MaxAge   time.Duration `json:"maxAge" conf:"log-max-age"`
	Backups  int           `json:"backups"`
	Rotation struct {
		Size int64 `json:"size"`
	} `json:"rotation"`
}

type dbConfig struct {
	Version uint32   `json:"version"`
	DSN     string   `json:"dsn" conf:"dsn,d"`
	Tags    []string `json:"tags" conf:"tag,t"`
}

// testControls mirrors the framework's core contribution: composite-
// core tests need a second contributor claiming the control keys.
type testControls struct {
	Disable  []string `json:"disable" conf:"disable" usage:"drop"`
	Enable   []string `json:"enable" conf:"enable" usage:"force"`
	Override []string `json:"override" conf:"override" usage:"remap"`
}

func coreWith(core *Core, ctl *testControls) []Contribution {
	return []Contribution{CoreContrib(core), {Ptr: ctl}}
}

func newTestSchema(t *testing.T, core *Core, structs map[string]any) *Schema {
	t.Helper()
	var sections []Section
	for name, cfg := range structs {
		sections = append(sections, Section{Name: name, Ptr: cfg})
	}
	c := &fail.Collector{}
	s := NewSchema(c, "cat", []Contribution{CoreContrib(core)}, sections, nil)
	if c.Len() != 0 {
		t.Fatalf("unexpected schema errors: %v", c.All())
	}
	return s
}

func TestSchemaExtraction(t *testing.T) {
	cfg := &sinkConfig{Version: 1}
	s := newTestSchema(t, &Core{}, map[string]any{"filesink": cfg})
	var svc *serviceSchema
	for _, candidate := range s.services {
		if candidate.id == "filesink" {
			svc = candidate
		}
	}
	if svc == nil || len(svc.fields) != 6 { // 5 declared + the mandated Version
		t.Fatalf("expected 5 fields, got %+v", svc)
	}
	byName := map[string]*Field{}
	for _, f := range svc.fields {
		byName[f.Name] = f
	}
	if f := byName["Level"]; f.EnvName != "LOG_LEVEL" || f.Short != "l" || f.Long != "log-level" {
		t.Errorf("explicit tags wrong: %+v", f)
	}
	if f := byName["MaxAge"]; f.EnvName != "CAT__LOG_MAX_AGE" {
		t.Errorf("derived env name wrong: %q", f.EnvName)
	}
	// Ledger note: the tag cutover gave untagged fields derived env
	// names, section-qualified (alias __ section __ json segments) —
	// file-only died as a concept; the invocation's own section skips
	// the qualifier
	if f := byName["Backups"]; f.Long != "" || f.EnvName != "CAT__FILESINK__BACKUPS" {
		t.Errorf("untagged field must derive its env name: %+v", f)
	}
	if f := byName["Rotation.Size"]; f == nil || !reflect.DeepEqual(f.JSONPath, []string{"rotation", "size"}) {
		t.Errorf("nested field wrong: %+v", f)
	} else if f.EnvName != "CAT__FILESINK__ROTATION__SIZE" {
		t.Errorf("nested fields derive path-stitched env names: %q", f.EnvName)
	}
}

func TestSchemaErrors(t *testing.T) {
	type missingJSON struct {
		X int `conf:"x-value"`
	}
	type badArg struct {
		X int `json:"x" conf:"X"`
	}
	type nestedArg struct {
		N struct {
			X int `json:"x" conf:"x-value"`
		} `json:"n"`
	}
	type unsupported struct {
		M map[string]string `json:"m"`
	}
	// built via StructOf: a literal duplicate json tag trips go vet
	dupJSON := reflect.New(reflect.StructOf([]reflect.StructField{
		{Name: "A", Type: reflect.TypeOf(0), Tag: `json:"same"`},
		{Name: "B", Type: reflect.TypeOf(0), Tag: `json:"same"`},
	})).Interface()
	type embedded struct {
		Core `json:"core"`
	}
	cases := []struct {
		name string
		cfg  any
		want string
	}{
		{"missing json tag", &missingJSON{}, "json tag"},
		{"invalid conf tag", &badArg{}, "invalid conf tag"},
		{"conf tag on nested field", &nestedArg{}, "below the top level"},
		{"unsupported type", &unsupported{}, "unsupported type"},
		{"duplicate json name", dupJSON, "duplicate json name"},
		{"embedded field", &embedded{}, "embedded"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateConfigType("svc", reflect.TypeOf(tc.cfg))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestSchemaCrossServiceCollisions(t *testing.T) {
	type one struct {
		X int `json:"x" conf:"shared"`
	}
	type two struct {
		Y int `json:"y" conf:"shared"`
	}
	sections := []Section{
		{Name: "one", Ptr: &one{}},
		{Name: "two", Ptr: &two{}},
	}
	c := &fail.Collector{}
	NewSchema(c, "cat", []Contribution{CoreContrib(&Core{})}, sections, nil)
	if c.Len() == 0 {
		t.Error("duplicate long across services must be an error")
	}
}

func TestSuppressedCoreFields(t *testing.T) {
	var core Core
	c := &fail.Collector{}
	// override lives in the second contribution: suppression spans
	// the whole composite core
	s := NewSchema(c, "cat", coreWith(&core, &testControls{}), nil, []string{"config", "override"})
	if c.Len() != 0 {
		t.Fatalf("unexpected schema errors: %v", c.All())
	}
	if _, present := s.long["config"]; present {
		t.Error("suppressed --config must not be in the schema")
	}
	if _, present := s.short["c"]; present {
		t.Error("suppressed field's short form must vanish too")
	}
	s.parseArgs(c, []string{"--config", "x.json"}, false)
	if c.Len() == 0 {
		t.Error("a suppressed argument must be unknown in the strict pass")
	}
	c2 := &fail.Collector{}
	s.applyEnv(c2, env(map[string]string{"CAT__CONFIG": "sneaky.json"}))
	if core.Config != "" || c2.Len() != 0 {
		t.Errorf("suppressed env var must be ignored: %q, %v", core.Config, c2.All())
	}
}

func TestSuppressUnknownNameFails(t *testing.T) {
	var core Core
	c := &fail.Collector{}
	NewSchema(c, "cat", []Contribution{CoreContrib(&core)}, nil, []string{"no-such-flag"})
	if c.Len() == 0 {
		t.Error("suppressing a non-existent core argument must fail")
	}
}

func TestShortFormFirstComeFirstServed(t *testing.T) {
	type wantsC struct {
		Version uint32 `json:"version"`
		X       int    `json:"x" conf:"x-value,c"` // "c" is already core's --config short
	}
	s := newTestSchema(t, &Core{}, map[string]any{"svc": &wantsC{Version: 1}})
	if s.short["c"].ServiceID != "core" {
		t.Errorf("core must keep -c, got %q", s.short["c"].ServiceID)
	}
	if s.long["x-value"].Short != "" {
		t.Error("loser of a short collision must have its short cleared")
	}
}

func TestCompositeCoreRejectsDuplicateKey(t *testing.T) {
	var core Core
	squatter := struct {
		Config string `json:"config"`
	}{}
	c := &fail.Collector{}
	NewSchema(c, "cat", []Contribution{CoreContrib(&core), {Ptr: &squatter}}, nil, nil)
	if c.Len() == 0 {
		t.Error("a core key claimed by two contributions must be a violation")
	}
}

// The cutover's grammar rules, pinned: __ is the structural stitch
// (never forgeable from within a name), camel humps fold to _, the
// arg tag is dead loudly, explicit env is legal at depth, and the
// version field carries json and nothing else.
func TestCutoverGrammar(t *testing.T) {
	type camel struct {
		Version uint32 `json:"version"`
		MaxAge  int    `json:"maxAge"`
	}
	s := newTestSchema(t, &Core{}, map[string]any{"svc": &camel{Version: 1}})
	var f *Field
	for _, svc := range s.services {
		for _, cand := range svc.fields {
			if cand.Name == "MaxAge" {
				f = cand
			}
		}
	}
	if f == nil || f.EnvName != "CAT__SVC__MAX_AGE" {
		t.Fatalf("camel humps fold to _, foreign sections qualify with __: %+v", f)
	}

	type deepEnv struct {
		Version uint32 `json:"version"`
		N       struct {
			X int `json:"x" env:"DEEP_X"`
		} `json:"n"`
	}
	c := &fail.Collector{}
	NewSchema(c, "cat", []Contribution{CoreContrib(&Core{})},
		[]Section{{Name: "svc", Ptr: &deepEnv{Version: 1}}}, nil)
	if c.Len() != 0 {
		t.Errorf("explicit env at depth is legal now: %v", c.All())
	}

	violations := []struct {
		name string
		cfg  any
		want string
	}{
		{"dead arg tag", &struct {
			Version uint32 `json:"version"`
			X       int    `json:"x" arg:"x-value"`
		}{Version: 1}, "the arg tag died"},
		{"separator run in conf name", &struct {
			Version uint32 `json:"version"`
			X       int    `json:"x" conf:"max--age"`
		}{Version: 1}, "invalid conf tag"},
		{"separator run in json name", &struct {
			Version uint32 `json:"version"`
			X       int    `json:"a--b"`
		}{Version: 1}, "forge a path boundary"},
		{"version field with conf tag", &struct {
			Version uint32 `json:"version" conf:"version"`
			X       int    `json:"x"`
		}{Version: 1}, "only the json annotation"},
		{"version field with env opt-out", &struct {
			Version uint32 `json:"version" env:"-"`
			X       int    `json:"x"`
		}{Version: 1}, "only the json annotation"},
	}
	for _, tc := range violations {
		t.Run(tc.name, func(t *testing.T) {
			c := &fail.Collector{}
			NewSchema(c, "cat", []Contribution{CoreContrib(&Core{})},
				[]Section{{Name: "svc", Ptr: tc.cfg}}, nil)
			found := false
			for _, err := range c.All() {
				found = found || strings.Contains(err.Error(), tc.want)
			}
			if !found {
				t.Errorf("want a violation containing %q, got %v", tc.want, c.All())
			}
		})
	}

	c2 := &fail.Collector{}
	NewSchema(c2, "cat", []Contribution{CoreContrib(&Core{})},
		[]Section{{Name: "svc--x", Ptr: &camel{Version: 1}}}, nil)
	found := false
	for _, err := range c2.All() {
		found = found || strings.Contains(err.Error(), "forge a path boundary")
	}
	if !found {
		t.Errorf("section names with separator runs must be violations: %v", c2.All())
	}
}

// The invocation's own section needs no qualifier: its untagged
// fields answer to the short address.
func TestActiveAppletSectionSkipsTheQualifier(t *testing.T) {
	type ownCfg struct {
		Version uint32 `json:"version"`
		Backups int    `json:"backups"`
	}
	c := &fail.Collector{}
	s := NewSchema(c, "cat", []Contribution{CoreContrib(&Core{})},
		[]Section{{Name: "cat", Ptr: &ownCfg{Version: 1}}}, nil)
	if c.Len() != 0 {
		t.Fatalf("unexpected errors: %v", c.All())
	}
	for _, svc := range s.services {
		for _, f := range svc.fields {
			if f.Name == "Backups" && f.EnvName != "CAT__BACKUPS" {
				t.Errorf("the active applet's section must skip the qualifier: %q", f.EnvName)
			}
		}
	}
}
