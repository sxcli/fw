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
	"bytes"
	"strings"
	"testing"

	"sxcli.dev/fw/internal/fail"
)

// grep-shaped: one required, one optional, a tail.
type posCfg struct {
	Version uint32   `json:"version"`
	Pattern string   `json:"pattern" pos:"0" usage:"the search pattern"`
	Mode    string   `json:"mode" pos:"1,optional"`
	Files   []string `json:"files" pos:"rest" usage:"files to search"`
}

func posSchema(t *testing.T, cfg any, args []string) (*fail.Collector, *Schema, Loaded) {
	t.Helper()
	c := &fail.Collector{}
	s := NewSchema(c, "grep", []Contribution{CoreContrib(&Core{})},
		[]Section{{Name: "grep", Ptr: cfg}}, nil)
	if c.Len() != 0 {
		t.Fatalf("unexpected schema errors: %v", c.All())
	}
	loaded := s.Apply(c, &Files{}, Sources{Args: args})
	return c, s, loaded
}

func TestPositionalsAssign(t *testing.T) {
	cfg := &posCfg{Version: 1}
	c, _, loaded := posSchema(t, cfg, []string{"needle", "fast", "a.txt", "b.txt"})
	if c.Len() != 0 {
		t.Fatalf("unexpected errors: %v", c.All())
	}
	if cfg.Pattern != "needle" || cfg.Mode != "fast" || strings.Join(cfg.Files, ",") != "a.txt,b.txt" {
		t.Errorf("assignment wrong: %+v", cfg)
	}
	if loaded.Positionals != nil {
		t.Errorf("a declaring schema owns the tail entirely: %v", loaded.Positionals)
	}
}

func TestPositionalsOptionalAndEmptyRest(t *testing.T) {
	cfg := &posCfg{Version: 1, Files: []string{"default.txt"}}
	c, _, _ := posSchema(t, cfg, []string{"needle"})
	if c.Len() != 0 {
		t.Fatalf("an optional positional may be absent: %v", c.All())
	}
	if cfg.Mode != "" || strings.Join(cfg.Files, ",") != "default.txt" {
		t.Errorf("absent optionals keep defaults: %+v", cfg)
	}
}

func TestPositionalsMissingRequired(t *testing.T) {
	c, _, _ := posSchema(t, &posCfg{Version: 1}, nil)
	if c.Len() == 0 || !strings.Contains(c.All()[0].Error(), "missing required positional <pattern>") {
		t.Errorf("a missing required positional must be named: %v", c.All())
	}
}

func TestPositionalsSurplusWithoutRest(t *testing.T) {
	type oneCfg struct {
		Version uint32 `json:"version"`
		Name    string `json:"name" pos:"0"`
	}
	c := &fail.Collector{}
	s := NewSchema(c, "one", []Contribution{CoreContrib(&Core{})},
		[]Section{{Name: "one", Ptr: &oneCfg{Version: 1}}}, nil)
	s.Apply(c, &Files{}, Sources{Args: []string{"a", "surplus"}})
	if c.Len() == 0 || !strings.Contains(c.All()[0].Error(), `unexpected positional "surplus"`) {
		t.Errorf("surplus without rest must be a violation: %v", c.All())
	}
}

func TestPositionalsUndeclaredTailPassesThrough(t *testing.T) {
	type plainCfg struct {
		Version uint32 `json:"version"`
		X       int    `json:"x"`
	}
	c := &fail.Collector{}
	s := NewSchema(c, "plain", []Contribution{CoreContrib(&Core{})},
		[]Section{{Name: "plain", Ptr: &plainCfg{Version: 1}}}, nil)
	loaded := s.Apply(c, &Files{}, Sources{Args: []string{"raw", "tail"}})
	if c.Len() != 0 {
		t.Fatalf("unexpected errors: %v", c.All())
	}
	if strings.Join(loaded.Positionals, ",") != "raw,tail" {
		t.Errorf("with nothing declared the raw tail passes through: %v", loaded.Positionals)
	}
}

func TestPositionalsDomainsApply(t *testing.T) {
	cfg := &posCfg{Version: 1}
	c := &fail.Collector{}
	s := NewSchema(c, "grep", []Contribution{CoreContrib(&Core{})},
		[]Section{{Name: "grep", Ptr: cfg, Meta: &Meta{Fields: map[string]FieldMeta{
			"Mode": {Allowed: []any{"fast", "slow"}},
		}}}}, nil)
	s.Apply(c, &Files{}, Sources{Args: []string{"needle", "turbo"}})
	if c.Len() == 0 || !strings.Contains(c.All()[0].Error(), "not among the allowed values") {
		t.Errorf("domains must bind positionals too: %v", c.All())
	}
}

func TestPositionalsRefusedFromOtherSources(t *testing.T) {
	// pos implies transient: a file mentioning the key is refused
	cfg := &posCfg{Version: 1}
	disk := map[string]string{"/etc/grep/config.json": `{"grep": {"pattern": "sneaky"}}`}
	src := Sources{
		Args:      []string{"needle"},
		Locations: []Location{{Base: "/etc/grep/config"}},
		Stat:      fakeStat(disk),
		Open:      fakeFS(disk),
	}
	c := &fail.Collector{}
	s := NewSchema(c, "grep", []Contribution{CoreContrib(&Core{})},
		[]Section{{Name: "grep", Ptr: cfg}}, nil)
	files := LoadFiles(c, src, "")
	s.Apply(c, files, src)
	if c.Len() == 0 || !strings.Contains(c.All()[0].Error(), "run-scoped") {
		t.Errorf("a positional key in a file must be refused: %v", c.All())
	}
}

func TestPositionalsRenderInHelp(t *testing.T) {
	cfg := &posCfg{Version: 1}
	c := &fail.Collector{}
	s := NewSchema(c, "grep", []Contribution{CoreContrib(&Core{})},
		[]Section{{Name: "grep", Ptr: cfg}}, nil)
	var out bytes.Buffer
	s.WriteHelp(&out)
	text := out.String()
	if !strings.Contains(text, "<pattern>") || !strings.Contains(text, "<mode> (optional)") || !strings.Contains(text, "<files...>") {
		t.Errorf("help must render the positional contract:\n%s", text)
	}
}

func TestPositionalGrammarViolations(t *testing.T) {
	cases := []struct {
		name string
		cfg  any
		want string
	}{
		{"duplicate index", &struct {
			Version uint32 `json:"version"`
			A       string `json:"a" pos:"0"`
			B       string `json:"b" pos:"0"`
		}{Version: 1}, "declared twice"},
		{"gap", &struct {
			Version uint32 `json:"version"`
			A       string `json:"a" pos:"1"`
		}{Version: 1}, "contiguous"},
		{"required after optional", &struct {
			Version uint32 `json:"version"`
			A       string `json:"a" pos:"0,optional"`
			B       string `json:"b" pos:"1"`
		}{Version: 1}, "ambiguous"},
		{"rest on scalar", &struct {
			Version uint32 `json:"version"`
			A       string `json:"a" pos:"rest"`
		}{Version: 1}, "requires a slice"},
		{"indexed slice", &struct {
			Version uint32   `json:"version"`
			A       []string `json:"a" pos:"0"`
		}{Version: 1}, "scalar"},
		{"flag and positional", &struct {
			Version uint32 `json:"version"`
			A       string `json:"a" conf:"a-flag" pos:"0"`
		}{Version: 1}, "never both"},
		{"env on positional", &struct {
			Version uint32 `json:"version"`
			A       string `json:"a" env:"A_VALUE" pos:"0"`
		}{Version: 1}, "argument-only"},
		{"two rests", &struct {
			Version uint32   `json:"version"`
			A       []string `json:"a" pos:"rest"`
			B       []string `json:"b" pos:"rest"`
		}{Version: 1}, "at most one"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &fail.Collector{}
			NewSchema(c, "svc", []Contribution{CoreContrib(&Core{})},
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
}

func TestPositionalsOfDormantSectionsStayDormant(t *testing.T) {
	// another applet's declarations in the closure are not this
	// invocation's contract
	active := &posCfg{Version: 1}
	type otherCfg struct {
		Version uint32 `json:"version"`
		Thing   string `json:"thing" pos:"0"`
	}
	other := &otherCfg{Version: 1}
	c := &fail.Collector{}
	s := NewSchema(c, "grep", []Contribution{CoreContrib(&Core{})},
		[]Section{{Name: "grep", Ptr: active}, {Name: "other", Ptr: other}}, nil)
	if c.Len() != 0 {
		t.Fatalf("unexpected errors: %v", c.All())
	}
	s.Apply(c, &Files{}, Sources{Args: []string{"needle"}})
	if c.Len() != 0 {
		t.Fatalf("the dormant declaration must not demand tokens: %v", c.All())
	}
	if other.Thing != "" || active.Pattern != "needle" {
		t.Errorf("only the active section binds: active=%+v other=%+v", active, other)
	}
}
