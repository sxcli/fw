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
	"strings"
	"testing"

	"sxcli.dev/fw/internal/fail"
)

// The schema history: v1 had one address field, v2 renamed it, the
// current v3 split host and port.
type srvV1 struct {
	Version uint32 `json:"version"`
	Addr    string `json:"addr"`
}

type srvV2 struct {
	Version uint32 `json:"version"`
	Listen  string `json:"listen"`
}

type srvV3 struct {
	Version uint32 `json:"version"`
	Host    string `json:"host" arg:"host"`
	Port    string `json:"port" arg:"port"`
}

func srvSteps() []Step {
	return []Step{
		NewStep(1, func(old srvV1) srvV2 { return srvV2{Listen: old.Addr} }),
		NewStep(2, func(old srvV2) srvV3 {
			host, port, _ := strings.Cut(old.Listen, ":")
			return srvV3{Host: host, Port: port}
		}),
	}
}

func migrateWorld(t *testing.T, cfg *srvV3, disk map[string]string, args []string, env map[string]string, steps []Step) (*fail.Collector, *Schema) {
	t.Helper()
	src := Sources{
		Args:      args,
		LookupEnv: envLookup(env),
		Locations: []Location{{Base: "/etc/srv/config"}, {Base: "/home/u/srv/config"}},
		Stat:      fakeStat(disk),
		Open:      fakeFS(disk),
	}
	c := &fail.Collector{}
	files := LoadFiles(c, src, "")
	if c.Len() != 0 {
		t.Fatalf("unexpected load errors: %v", c.All())
	}
	sch := NewSchema(c, "srv", []Contribution{CoreContrib(&Core{})},
		[]Section{{Name: "srv", Ptr: cfg, Steps: steps}}, nil)
	if c.Len() != 0 {
		t.Fatalf("unexpected schema errors: %v", c.All())
	}
	sch.Apply(c, files, src)
	return c, sch
}

func envLookup(env map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := env[name]
		return v, ok
	}
}

func TestMigrationWalksTheChain(t *testing.T) {
	cfg := &srvV3{Version: 3, Host: "localhost", Port: "80"}
	disk := map[string]string{"/etc/srv/config.json": `{"srv": {"version": 1, "addr": "example.com:8080"}}`}
	c, _ := migrateWorld(t, cfg, disk, nil, nil, srvSteps())
	if c.Len() != 0 {
		t.Fatalf("unexpected errors: %v", c.All())
	}
	if cfg.Host != "example.com" || cfg.Port != "8080" {
		t.Errorf("migration wrong: %+v", cfg)
	}
	if cfg.Version != 3 {
		t.Errorf("the engine must stamp the current version: %d", cfg.Version)
	}
}

func TestMigratedValuesLoseToLaterSources(t *testing.T) {
	cfg := &srvV3{Version: 3}
	disk := map[string]string{"/etc/srv/config.json": `{"srv": {"version": 1, "addr": "example.com:8080"}}`}
	c, _ := migrateWorld(t, cfg, disk, []string{"--port", "9090"},
		map[string]string{"SRV_HOST": "envhost"}, srvSteps())
	if c.Len() != 0 {
		t.Fatalf("unexpected errors: %v", c.All())
	}
	if cfg.Host != "envhost" || cfg.Port != "9090" {
		t.Errorf("env and args must beat migrated file values: %+v", cfg)
	}
}

func TestVersionedDocumentIsComplete(t *testing.T) {
	// the lower-precedence v1 doc is COMPLETE: its migrated zero Port
	// must shade the default; the later versionless partial then
	// overrides key-by-key
	cfg := &srvV3{Version: 3, Port: "443"}
	disk := map[string]string{
		"/etc/srv/config.json":    `{"srv": {"version": 1, "addr": "onlyhost"}}`,
		"/home/u/srv/config.json": `{"srv": {"host": "userhost"}}`,
	}
	c, _ := migrateWorld(t, cfg, disk, nil, nil, srvSteps())
	if c.Len() != 0 {
		t.Fatalf("unexpected errors: %v", c.All())
	}
	if cfg.Port != "" {
		t.Errorf("a versioned document is complete — its zero must shade the default: %+v", cfg)
	}
	if cfg.Host != "userhost" {
		t.Errorf("the later versionless partial must win key-by-key: %+v", cfg)
	}
}

func TestOldDocumentJudgedByItsOwnSchema(t *testing.T) {
	cfg := &srvV3{Version: 3}
	disk := map[string]string{"/etc/srv/config.json": `{"srv": {"version": 1, "addr": "x", "host": "not-in-v1"}}`}
	c, _ := migrateWorld(t, cfg, disk, nil, nil, srvSteps())
	if c.Len() == 0 {
		t.Fatal("a v1 document with a v3 key must fail the v1 strict parse")
	}
	if !strings.Contains(c.All()[0].Error(), "version 1") {
		t.Errorf("the violation must name the judging version: %v", c.All())
	}
}

func TestNewerThanBinaryIsRefused(t *testing.T) {
	cfg := &srvV3{Version: 3}
	disk := map[string]string{"/etc/srv/config.json": `{"srv": {"version": 9}}`}
	c, _ := migrateWorld(t, cfg, disk, nil, nil, srvSteps())
	if c.Len() == 0 || !strings.Contains(c.All()[0].Error(), "newer schema") {
		t.Errorf("a future version must be refused loudly: %v", c.All())
	}
}

func TestOlderThanOldestStepIsRefused(t *testing.T) {
	cfg := &srvV3{Version: 3}
	disk := map[string]string{"/etc/srv/config.json": `{"srv": {"version": 1, "addr": "x"}}`}
	// the chain starts at 2: version 1 support was dropped
	c, _ := migrateWorld(t, cfg, disk, nil, nil, []Step{
		NewStep(2, func(old srvV2) srvV3 { return srvV3{Host: old.Listen} }),
	})
	if c.Len() == 0 || !strings.Contains(c.All()[0].Error(), "no longer supported") {
		t.Errorf("dropped versions must be refused by name: %v", c.All())
	}
}

func TestChainlessVersionMismatchIsRefused(t *testing.T) {
	cfg := &srvV3{Version: 3}
	disk := map[string]string{"/etc/srv/config.json": `{"srv": {"version": 2, "listen": "x"}}`}
	c, _ := migrateWorld(t, cfg, disk, nil, nil, nil)
	if c.Len() == 0 || !strings.Contains(c.All()[0].Error(), "no migration") {
		t.Errorf("a version mismatch without a chain must be refused: %v", c.All())
	}
}

func TestChainValidation(t *testing.T) {
	bad := []struct {
		name  string
		def   uint32
		steps []Step
		want  string
	}{
		{"gap in the chain", 4, []Step{
			NewStep(1, func(old srvV1) srvV2 { return srvV2{} }),
			NewStep(3, func(old srvV2) srvV3 { return srvV3{} }),
		}, "contiguous"},
		{"type mismatch between steps", 3, []Step{
			NewStep(1, func(old srvV1) srvV1 { return old }),
			NewStep(2, func(old srvV2) srvV3 { return srvV3{} }),
		}, "produces"},
		{"terminal is not the current type", 3, []Step{
			NewStep(1, func(old srvV1) srvV2 { return srvV2{} }),
		}, "terminate at the current config type"},
		{"default does not match the terminal", 7, srvSteps(), "terminal version"},
		{"factory forgot the default", 0, nil, "versions start at 1"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			c := &fail.Collector{}
			NewSchema(c, "srv", []Contribution{CoreContrib(&Core{})},
				[]Section{{Name: "srv", Ptr: &srvV3{Version: tc.def}, Steps: tc.steps}}, nil)
			found := false
			for _, err := range c.All() {
				found = found || strings.Contains(err.Error(), tc.want)
			}
			if !found {
				t.Errorf("want a violation containing %q, got %v", tc.want, c.All())
			}
		})
	}
}
