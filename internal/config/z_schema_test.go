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

package config

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"sxcli.dev/fw/internal/fail"
	"sxcli.dev/fw/internal/registry"
)

type sinkConfig struct {
	Path     string        `json:"path" arg:"log-path" usage:"log file location"`
	Level    string        `json:"level" arg:"log-level,l" env:"LOG_LEVEL" usage:"minimum level"`
	MaxAge   time.Duration `json:"maxAge" arg:"log-max-age"`
	Backups  int           `json:"backups"`
	Rotation struct {
		Size int64 `json:"size"`
	} `json:"rotation"`
}

type dbConfig struct {
	DSN  string   `json:"dsn" arg:"dsn,d"`
	Tags []string `json:"tags" arg:"tag,t"`
}

func newTestSchema(t *testing.T, core *Core, structs map[string]any) *Schema {
	t.Helper()
	var members []*registry.Descriptor
	for id, cfg := range structs {
		members = append(members, &registry.Descriptor{ID: id, Aliases: []string{id}, ConfigPtr: cfg})
	}
	c := &fail.Collector{}
	s := NewSchema(c, "cat", core, members, nil)
	if c.Len() != 0 {
		t.Fatalf("unexpected schema errors: %v", c.All())
	}
	return s
}

func TestSchemaExtraction(t *testing.T) {
	cfg := &sinkConfig{}
	s := newTestSchema(t, &Core{}, map[string]any{"filesink": cfg})
	var svc *serviceSchema
	for _, candidate := range s.services {
		if candidate.id == "filesink" {
			svc = candidate
		}
	}
	if svc == nil || len(svc.fields) != 5 {
		t.Fatalf("expected 5 fields, got %+v", svc)
	}
	byName := map[string]*Field{}
	for _, f := range svc.fields {
		byName[f.Name] = f
	}
	if f := byName["Level"]; f.EnvName != "LOG_LEVEL" || f.Short != "l" || f.Long != "log-level" {
		t.Errorf("explicit tags wrong: %+v", f)
	}
	if f := byName["MaxAge"]; f.EnvName != "CAT_LOG_MAX_AGE" {
		t.Errorf("derived env name wrong: %q", f.EnvName)
	}
	if f := byName["Backups"]; f.Long != "" || f.EnvName != "" {
		t.Errorf("untagged field must be file-only: %+v", f)
	}
	if f := byName["Rotation.Size"]; f == nil || !reflect.DeepEqual(f.JSONPath, []string{"rotation", "size"}) {
		t.Errorf("nested field wrong: %+v", f)
	}
}

func TestSchemaErrors(t *testing.T) {
	type missingJSON struct {
		X int `arg:"x-value"`
	}
	type badArg struct {
		X int `json:"x" arg:"X"`
	}
	type nestedArg struct {
		N struct {
			X int `json:"x" arg:"x-value"`
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
		{"invalid arg tag", &badArg{}, "invalid arg tag"},
		{"arg tag on nested field", &nestedArg{}, "file-only"},
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
		X int `json:"x" arg:"shared"`
	}
	type two struct {
		Y int `json:"y" arg:"shared"`
	}
	members := []*registry.Descriptor{
		{ID: "one", Aliases: []string{"one"}, ConfigPtr: &one{}},
		{ID: "two", Aliases: []string{"two"}, ConfigPtr: &two{}},
	}
	c := &fail.Collector{}
	NewSchema(c, "cat", &Core{}, members, nil)
	if c.Len() == 0 {
		t.Error("duplicate long across services must be an error")
	}
}

func TestSuppressedCoreFields(t *testing.T) {
	var core Core
	c := &fail.Collector{}
	s := NewSchema(c, "cat", &core, nil, []string{"config", "override"})
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
	s.applyEnv(c2, env(map[string]string{"CAT_CONFIG": "sneaky.json"}))
	if core.Config != "" || c2.Len() != 0 {
		t.Errorf("suppressed env var must be ignored: %q, %v", core.Config, c2.All())
	}
}

func TestSuppressUnknownNameFails(t *testing.T) {
	var core Core
	c := &fail.Collector{}
	NewSchema(c, "cat", &core, nil, []string{"no-such-flag"})
	if c.Len() == 0 {
		t.Error("suppressing a non-existent core argument must fail")
	}
}

func TestShortFormFirstComeFirstServed(t *testing.T) {
	type wantsC struct {
		X int `json:"x" arg:"x-value,c"` // "c" is already core's --config short
	}
	s := newTestSchema(t, &Core{}, map[string]any{"svc": &wantsC{}})
	if s.short["c"].ServiceID != "core" {
		t.Errorf("core must keep -c, got %q", s.short["c"].ServiceID)
	}
	if s.long["x-value"].Short != "" {
		t.Error("loser of a short collision must have its short cleared")
	}
}
