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
	"testing"
	"time"

	"sxcli.dev/fw/internal/fail"
)

type argsConfig struct {
	Name    string        `json:"name" arg:"name,n"`
	Verbose bool          `json:"verbose" arg:"verbose,v"`
	Debug   bool          `json:"debug" arg:"debug,x"`
	Count   int           `json:"count" arg:"count,q"`
	Wait    time.Duration `json:"wait" arg:"wait"`
	Tags    []string      `json:"tags" arg:"tag,t"`
}

func parseInto(t *testing.T, cfg *argsConfig, lenient bool, args ...string) ([]string, []error) {
	t.Helper()
	s := newTestSchema(t, &Core{}, map[string]any{"svc": cfg})
	c := &fail.Collector{}
	pos := s.parseArgs(c, args, lenient)
	return pos, c.All()
}

func mustParse(t *testing.T, cfg *argsConfig, args ...string) []string {
	t.Helper()
	pos, errs := parseInto(t, cfg, false, args...)
	if len(errs) != 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	return pos
}

func TestArgForms(t *testing.T) {
	cfg := &argsConfig{}
	mustParse(t, cfg, "--name=alpha", "--count", "3", "-q=5", "--wait", "1h30m")
	if cfg.Name != "alpha" || cfg.Count != 5 || cfg.Wait != 90*time.Minute {
		t.Errorf("values wrong: %+v", cfg)
	}
}

func TestBoolSemantics(t *testing.T) {
	cfg := &argsConfig{}
	pos := mustParse(t, cfg, "--verbose", "trailing")
	if !cfg.Verbose {
		t.Error("bare bool presence must mean true")
	}
	if !reflect.DeepEqual(pos, []string{"trailing"}) {
		t.Errorf("a bool must not consume a separated value: %v", pos)
	}
	cfg = &argsConfig{Verbose: true}
	mustParse(t, cfg, "--verbose=false")
	if cfg.Verbose {
		t.Error("=false must unset")
	}
}

func TestBundling(t *testing.T) {
	cfg := &argsConfig{}
	mustParse(t, cfg, "-vxq", "7")
	if !cfg.Verbose || !cfg.Debug || cfg.Count != 7 {
		t.Errorf("bundle with trailing value wrong: %+v", cfg)
	}
	cfg = &argsConfig{}
	mustParse(t, cfg, "-vxq=9")
	if cfg.Count != 9 {
		t.Errorf("bundle with = value wrong: %+v", cfg)
	}
	cfg = &argsConfig{}
	if _, errs := parseInto(t, cfg, false, "-qv", "3"); len(errs) == 0 {
		t.Error("non-bool in the middle of a bundle must be an error")
	}
}

func TestSliceRepetition(t *testing.T) {
	cfg := &argsConfig{Tags: []string{"from-env"}}
	mustParse(t, cfg, "--tag", "a", "-t", "b")
	if !reflect.DeepEqual(cfg.Tags, []string{"a", "b"}) {
		t.Errorf("first occurrence must reset, repetitions append: %v", cfg.Tags)
	}
}

func TestPositionalRules(t *testing.T) {
	cfg := &argsConfig{}
	pos := mustParse(t, cfg, "--name", "x", "one", "two")
	if !reflect.DeepEqual(pos, []string{"one", "two"}) {
		t.Errorf("trailing bare tokens are positionals: %v", pos)
	}
	if _, errs := parseInto(t, &argsConfig{}, false, "one", "--name", "x"); len(errs) == 0 {
		t.Error("bare token before a flag must be an error in strict mode")
	}
	pos = mustParse(t, &argsConfig{}, "--", "--name", "x")
	if !reflect.DeepEqual(pos, []string{"--name", "x"}) {
		t.Errorf("everything after -- is positional: %v", pos)
	}
}

func TestStrictErrors(t *testing.T) {
	if _, errs := parseInto(t, &argsConfig{}, false, "--ghost"); len(errs) == 0 {
		t.Error("unknown long must be an error")
	}
	if _, errs := parseInto(t, &argsConfig{}, false, "-z"); len(errs) == 0 {
		t.Error("unknown short must be an error")
	}
	if _, errs := parseInto(t, &argsConfig{}, false, "--name"); len(errs) == 0 {
		t.Error("missing value must be an error")
	}
	if _, errs := parseInto(t, &argsConfig{}, false, "--count", "abc"); len(errs) == 0 {
		t.Error("invalid value must be an error")
	}
	if _, errs := parseInto(t, &argsConfig{}, false, "--wait", "5000"); len(errs) == 0 {
		t.Error("bare-number duration must be an error")
	}
}

func TestLenientSkipsUnknown(t *testing.T) {
	var core Core
	c := &fail.Collector{}
	s := NewSchema(c, "cat", []Contribution{CoreContrib(&core)}, nil, nil)
	s.parseArgs(c, []string{"--level", "warn", "-z", "--config", "override.json", "stray"}, true)
	if c.Len() != 0 {
		t.Fatalf("lenient mode must not error on unknowns: %v", c.All())
	}
	if core.Config != "override.json" {
		t.Errorf("known core value not extracted: %q", core.Config)
	}
}
