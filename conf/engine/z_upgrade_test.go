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
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sxcli.dev/fw/internal/fail"
)

// upgradeWorld writes content to a real temp file and builds a schema
// owning the srv section (the z_migrate fixtures) plus real-fs Sources.
func upgradeWorld(t *testing.T, content string, steps []Step) (string, *Schema, Sources) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
	src := Sources{
		Stat: StatRegular,
		Open: func(p string) (io.ReadCloser, error) { return os.Open(p) },
	}
	c := &fail.Collector{}
	sch := NewSchema(c, "srv", []Contribution{CoreContrib(&Core{})},
		[]Section{{Name: "srv", Ptr: &srvV3{Version: 3}, Steps: steps}}, nil)
	if c.Len() != 0 {
		t.Fatalf("unexpected schema errors: %v", c.All())
	}
	return path, sch, src
}

func upgraded(t *testing.T, path string) map[string]map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("upgraded file is not valid json: %v\n%s", err, raw)
	}
	return out
}

func TestUpgradeMigratesByFileVersion(t *testing.T) {
	content := `{"srv": {"version": 1, "addr": "example.com:8080"}, "foreign": {"keep": true}}`
	path, sch, src := upgradeWorld(t, content, srvSteps())
	c := &fail.Collector{}
	sch.UpgradeFile(c, path, map[string]uint32{}, nil, src)
	if c.Len() != 0 {
		t.Fatalf("unexpected errors: %v", c.All())
	}
	out := upgraded(t, path)
	srv := out["srv"]
	if srv["version"] != float64(3) || srv["host"] != "example.com" || srv["port"] != "8080" {
		t.Errorf("migration wrong: %v", srv)
	}
	if _, stale := srv["addr"]; stale {
		t.Errorf("the old key must be gone: %v", srv)
	}
	if out["foreign"]["keep"] != true {
		t.Errorf("foreign sections must pass through verbatim: %v", out)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o640 {
		t.Errorf("the file mode must be preserved: %v", fi.Mode())
	}
}

func TestUpgradeVersionlessNeedsAssertion(t *testing.T) {
	path, sch, src := upgradeWorld(t, `{"srv": {"host": "x"}}`, srvSteps())
	c := &fail.Collector{}
	sch.UpgradeFile(c, path, map[string]uint32{}, nil, src)
	if c.Len() == 0 || !strings.Contains(c.All()[0].Error(), "--from-version") {
		t.Errorf("a versionless section must demand the assertion: %v", c.All())
	}
}

func TestUpgradeScopedAssertionMigrates(t *testing.T) {
	path, sch, src := upgradeWorld(t, `{"srv": {"addr": "h:1"}}`, srvSteps())
	c := &fail.Collector{}
	sch.UpgradeFile(c, path, map[string]uint32{"srv": 1}, nil, src)
	if c.Len() != 0 {
		t.Fatalf("unexpected errors: %v", c.All())
	}
	srv := upgraded(t, path)["srv"]
	if srv["version"] != float64(3) || srv["host"] != "h" {
		t.Errorf("asserted migration wrong: %v", srv)
	}
}

func TestUpgradeBareAssertion(t *testing.T) {
	path, sch, src := upgradeWorld(t, `{"srv": {"addr": "h:1"}}`, srvSteps())
	one := uint32(1)
	c := &fail.Collector{}
	sch.UpgradeFile(c, path, map[string]uint32{}, &one, src)
	if c.Len() != 0 {
		t.Fatalf("a lone versionless section must accept the bare form: %v", c.All())
	}
	if upgraded(t, path)["srv"]["host"] != "h" {
		t.Error("bare assertion must migrate")
	}
}

func TestUpgradeContradictionRefused(t *testing.T) {
	path, sch, src := upgradeWorld(t, `{"srv": {"version": 2, "listen": "x"}}`, srvSteps())
	c := &fail.Collector{}
	sch.UpgradeFile(c, path, map[string]uint32{"srv": 1}, nil, src)
	if c.Len() == 0 || !strings.Contains(c.All()[0].Error(), "not a tiebreak") {
		t.Errorf("a contradicting assertion must be refused: %v", c.All())
	}
}

func TestUpgradeCurrentIsRestamped(t *testing.T) {
	path, sch, src := upgradeWorld(t, `{"srv": {"version": 3, "host": "h"}}`, srvSteps())
	c := &fail.Collector{}
	sch.UpgradeFile(c, path, map[string]uint32{}, nil, src)
	if c.Len() != 0 {
		t.Fatalf("unexpected errors: %v", c.All())
	}
	srv := upgraded(t, path)["srv"]
	if srv["version"] != float64(3) || srv["host"] != "h" {
		t.Errorf("current document must survive: %v", srv)
	}
}

func TestUpgradeUnknownFromTarget(t *testing.T) {
	path, sch, src := upgradeWorld(t, `{"srv": {"version": 3}}`, srvSteps())
	c := &fail.Collector{}
	sch.UpgradeFile(c, path, map[string]uint32{"ghost": 1}, nil, src)
	if c.Len() == 0 || !strings.Contains(c.All()[0].Error(), "ghost") {
		t.Errorf("an unknown --from-version target must be loud: %v", c.All())
	}
}
