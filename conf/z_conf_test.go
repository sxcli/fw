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

package conf

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sxcli.dev/fw/conf/engine"
)

type toolCfg struct {
	Version uint32        `json:"version"`
	Listen  string        `json:"listen" arg:"listen,l" usage:"address to serve on"`
	Timeout time.Duration `json:"timeout" arg:"timeout"`
	Token   string        `json:"token" env:"TOOL_TOKEN"`
}

// world builds a hermetic front-door run: fake argv/env/fs, captured
// output.
type world struct {
	stdout bytes.Buffer
	stderr bytes.Buffer
}

func (w *world) builder(t *testing.T, args []string, files, env map[string]string) *Loader {
	t.Helper()
	src := engine.Sources{
		Args: args,
		LookupEnv: func(name string) (string, bool) {
			v, ok := env[name]
			return v, ok
		},
		Locations: []engine.Location{{Base: "/etc/mytool/config"}},
		Stat: func(path string) (int64, error) {
			if content, ok := files[path]; ok {
				return int64(len(content)), nil
			}
			return 0, fs.ErrNotExist
		},
		Open: func(path string) (io.ReadCloser, error) {
			if content, ok := files[path]; ok {
				return io.NopCloser(strings.NewReader(content)), nil
			}
			return nil, fs.ErrNotExist
		},
		OpenPinned: func(path string) (io.ReadCloser, error) { return nil, fs.ErrNotExist },
	}
	return NewLoader("mytool").Section("mytool", &toolCfg{Version: 1}).Sources(src).Output(&w.stdout, &w.stderr)
}

func TestLoadFillsAndReturnsPositionals(t *testing.T) {
	w := &world{}
	cfg := &toolCfg{Version: 1, Listen: ":8080"}
	files := map[string]string{"/etc/mytool/config.json": `{"mytool": {"timeout": "5s"}}`}
	b := w.builder(t, []string{"--listen", ":9090", "one", "two"}, files, nil)
	b.sections = []engine.Section{{Name: "mytool", Ptr: cfg}}
	ldr, served := b.Load()
	if served {
		t.Fatal("a plain run must not be served")
	}
	pos, err := ldr.Result()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Listen != ":9090" || cfg.Timeout != 5*time.Second {
		t.Errorf("merge wrong: %+v", cfg)
	}
	if strings.Join(pos, ",") != "one,two" {
		t.Errorf("positionals wrong: %v", pos)
	}
}

func TestEnvSpeaksTheSectionPrefix(t *testing.T) {
	w := &world{}
	cfg := &toolCfg{Version: 1}
	b := w.builder(t, nil, nil, map[string]string{"MYTOOL_LISTEN": ":7070"})
	b.sections = []engine.Section{{Name: "mytool", Ptr: cfg}}
	ldr, _ := b.Load()
	if _, err := ldr.Result(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Listen != ":7070" {
		t.Errorf("env not applied: %+v", cfg)
	}
}

func TestHelpIsServedAndLeavesTheStructAlone(t *testing.T) {
	w := &world{}
	cfg := &toolCfg{Version: 1, Listen: ":8080"}
	b := w.builder(t, []string{"--help", "--listen", ":9090"}, nil, nil)
	b.sections = []engine.Section{{Name: "mytool", Ptr: cfg}}
	ldr, served := b.Load()
	if !served {
		t.Fatal("--help must be served")
	}
	if !strings.Contains(w.stdout.String(), "--listen") {
		t.Errorf("help must list the arguments:\n%s", w.stdout.String())
	}
	if cfg.Listen != ":8080" {
		t.Errorf("a served run must leave the struct untouched: %+v", cfg)
	}
	if _, err := ldr.Result(); err == nil {
		t.Error("loading a served run must be a loud misuse error")
	}
}

func TestHelpIsBestEffortOverBrokenFiles(t *testing.T) {
	w := &world{}
	files := map[string]string{"/etc/mytool/config.json": `{"mytool": {"nope": true}}`}
	ldr, served := w.builder(t, []string{"--help"}, files, nil).Load()
	if !served {
		t.Fatal("a broken config file must never take --help down")
	}
	if !strings.Contains(w.stdout.String(), "--listen") {
		t.Errorf("help must still render:\n%s", w.stdout.String())
	}
	if !strings.Contains(w.stderr.String(), "unknown key") {
		t.Errorf("the violations must not be swallowed:\n%s", w.stderr.String())
	}
	if _, err := ldr.Result(); err == nil {
		t.Error("reading a served run's verdict must be a loud misuse error")
	}
}

func TestWriteConfigServesTheMerge(t *testing.T) {
	w := &world{}
	cfg := &toolCfg{Version: 1, Listen: ":8080"}
	b := w.builder(t, []string{"--write-config", "--listen", ":9090"}, nil, nil)
	b.sections = []engine.Section{{Name: "mytool", Ptr: cfg}}
	_, served := b.Load()
	if !served {
		t.Fatal("--write-config must be served")
	}
	if !strings.Contains(w.stdout.String(), `":9090"`) {
		t.Errorf("emitted config must hold the merged value:\n%s", w.stdout.String())
	}
	if cfg.Listen != ":8080" {
		t.Errorf("a served run must leave the struct untouched: %+v", cfg)
	}
}

func TestWriteConfigRefusesAViolatedMerge(t *testing.T) {
	w := &world{}
	files := map[string]string{"/etc/mytool/config.json": `{"mytool": {"nope": true}}`}
	ldr, served := w.builder(t, []string{"--write-config"}, files, nil).Load()
	if served {
		t.Fatal("write-config must not emit from a violated merge")
	}
	if _, err := ldr.Result(); err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Errorf("the violations must arrive via Load: %v", err)
	}
}

func TestViolationsRestoreTheDefaults(t *testing.T) {
	w := &world{}
	cfg := &toolCfg{Version: 1, Listen: ":8080"}
	files := map[string]string{"/etc/mytool/config.json": `{"mytool": {"listen": ":6060", "nope": true}}`}
	b := w.builder(t, nil, files, nil)
	b.sections = []engine.Section{{Name: "mytool", Ptr: cfg}}
	ldr, served := b.Load()
	if served {
		t.Fatal("a violated run is not served")
	}
	if _, err := ldr.Result(); err == nil {
		t.Fatal("violations must surface")
	}
	if cfg.Listen != ":8080" {
		t.Errorf("on error the struct must hold its defaults: %+v", cfg)
	}
}

func TestSuppressRemovesTheSurface(t *testing.T) {
	w := &world{}
	b := w.builder(t, []string{"--write-config"}, nil, nil).Suppress(FeatureWriteConfig)
	ldr, served := b.Load()
	if served {
		t.Fatal("a suppressed write-config must not serve")
	}
	if _, err := ldr.Result(); err == nil || !strings.Contains(err.Error(), "write-config") {
		t.Errorf("the unknown argument must be a violation: %v", err)
	}
}

func TestTierSuppressionPrunesTheSearch(t *testing.T) {
	b := NewLoader("mytool").Suppress(FeatureCompanionConfig, FeatureUserConfig)
	locs := b.locations()
	if len(locs) != 1 || !strings.Contains(locs[0].Base, "mytool") || locs[0].Pinned {
		t.Errorf("only the system tier must remain: %+v", locs)
	}
	if len(NewLoader("mytool").Suppress(FeatureCompanionConfig, FeatureSystemConfig, FeatureUserConfig).locations()) != 0 {
		t.Error("suppressing every tier must leave no file search")
	}
	// tier features must not leak into the core-argument suppression
	src := NewLoader("mytool").Suppress(FeatureUserConfig, FeatureHelp).sources()
	if len(src.SuppressCore) != 1 || src.SuppressCore[0] != "help" {
		t.Errorf("only argument features belong in SuppressCore: %v", src.SuppressCore)
	}
}

func TestEnvironmentSuppressionKillsTheWholeSource(t *testing.T) {
	w := &world{}
	cfg := &toolCfg{Version: 1, Listen: ":8080"}
	env := map[string]string{"MYTOOL_LISTEN": ":7070", "TOOL_TOKEN": "sekrit"}
	b := w.builder(t, nil, nil, env).Suppress(FeatureEnvironment)
	b.sections = []engine.Section{{Name: "mytool", Ptr: cfg}}
	ldr, _ := b.Load()
	if _, err := ldr.Result(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Listen != ":8080" {
		t.Errorf("derived env name must be dead: %+v", cfg)
	}
	if cfg.Token != "" {
		t.Errorf("explicit env binding must die with the source: %+v", cfg)
	}
}

func TestMigrateWiresTheChain(t *testing.T) {
	type cfgV1 struct {
		Version uint32 `json:"version"`
		Addr    string `json:"addr"`
	}
	type cfgV2 struct {
		Version uint32 `json:"version"`
		Listen  string `json:"listen" arg:"listen"`
	}
	w := &world{}
	cfg := &cfgV2{Version: 2}
	files := map[string]string{"/etc/mytool/config.json": `{"tool": {"version": 1, "addr": ":8080"}}`}
	b := w.builder(t, nil, files, nil)
	b.sections = []engine.Section{{Name: "tool", Ptr: cfg}}
	b.Migrate("tool", Step(1, func(old cfgV1) cfgV2 { return cfgV2{Listen: old.Addr} }))
	ldr, served := b.Load()
	if served {
		t.Fatal("a migrating run is not served")
	}
	if _, err := ldr.Result(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Listen != ":8080" || cfg.Version != 2 {
		t.Errorf("migration through the front door wrong: %+v", cfg)
	}
	if _, badServed := w.builder(t, nil, nil, nil).Migrate("ghost").Load(); badServed {
		t.Fatal("unknown-section Migrate must not serve")
	} else if ldr2, _ := w.builder(t, nil, nil, nil).Migrate("ghost").Load(); ldr2 != nil {
		if _, err := ldr2.Result(); err == nil || !strings.Contains(err.Error(), "unknown section") {
			t.Errorf("Migrate on an unknown section must be a violation: %v", err)
		}
	}
}

func TestUpgradeConfigIsAPureTransform(t *testing.T) {
	type upV1 struct {
		Version uint32 `json:"version"`
		Addr    string `json:"addr"`
	}
	type upV2 struct {
		Version uint32 `json:"version"`
		Listen  string `json:"listen" arg:"listen"`
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"tool": {"addr": ":8080"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	w := &world{}
	cfg := &upV2{Version: 2, Listen: ":default"}
	src := engine.Sources{
		Args: []string{"--upgrade-config", "--config", path, "--from-version", "1"},
		// the invoking environment must never leak into the file
		LookupEnv: func(string) (string, bool) { return "poison", true },
		Stat:      engine.StatRegular,
		Open:      func(p string) (io.ReadCloser, error) { return os.Open(p) },
	}
	b := NewLoader("tool").Section("tool", cfg).Sources(src).Output(&w.stdout, &w.stderr).
		Migrate("tool", Step(1, func(old upV1) upV2 { return upV2{Listen: old.Addr} }))
	ldr, served := b.Load()
	if !served {
		if ldr != nil {
			_, err := ldr.Result()
			t.Fatalf("upgrade-config must be served: %v", err)
		}
		t.Fatal("upgrade-config must be served")
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), `"listen"`) || !strings.Contains(string(raw), `":8080"`) || strings.Contains(string(raw), "poison") {
		t.Errorf("the file must be migrated and pure:\n%s", raw)
	}
	if cfg.Listen != ":default" {
		t.Errorf("a served run must leave the struct untouched: %+v", cfg)
	}
}

func TestUpgradeConfigRequiresTarget(t *testing.T) {
	w := &world{}
	ldr, served := w.builder(t, []string{"--upgrade-config"}, nil, nil).Load()
	if served {
		t.Fatal("upgrade-config without a target must not serve")
	}
	if _, err := ldr.Result(); err == nil || !strings.Contains(err.Error(), "requires an explicit --config") {
		t.Errorf("the missing target must be the violation: %v", err)
	}
}

func TestResultBeforeLoadIsMisuse(t *testing.T) {
	if _, err := NewLoader("mytool").Result(); err == nil || !strings.Contains(err.Error(), "before Load") {
		t.Errorf("Result before Load must be a loud misuse error: %v", err)
	}
}
