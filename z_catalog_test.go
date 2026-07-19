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

package fw

import (
	"strings"
	"testing"

	"sxcli.dev/fw/internal/fail"
	"sxcli.dev/fw/internal/registry"
)

type catCfg struct {
	Version uint32 `json:"version"`
	Level   string `json:"level" conf:"cat-level" usage:"verbosity"`
}

type catService struct {
	cfg   catCfg
	built *int
}

type catIface interface{ Cat() }

func (s *catService) Cat() {}

type catApplet struct{ cfg catCfg }

func (a *catApplet) Configured() error { return nil }
func (a *catApplet) Run() int          { return 0 }

// catalogWorld is a private catalog + collector pair.
func catalogWorld() (*registry.Registry, *fail.Collector) {
	c := &fail.Collector{}
	return registry.New(c), c
}

// chain builds a valid, committable registration for a catService
// counting its constructions.
func chain(id string, built *int) *Registration[catService] {
	return NewRegistration(id, func() *catService {
		*built++
		return &catService{built: built, cfg: catCfg{Version: 1, Level: "info"}}
	}, func(s *catService) *catCfg { return &s.cfg }).Alias("cat")
}

func TestCommitHoldsNoState(t *testing.T) {
	reg, c := catalogWorld()
	built := 0
	chain("example.com/x/cat", &built).registerInto(reg, c)
	if c.Len() != 0 {
		t.Fatalf("unexpected violations: %v", c.All())
	}
	if built != 0 {
		t.Fatalf("committing must not construct: %d constructions", built)
	}
	d, ok := reg.ByID("example.com/x/cat")
	if !ok || d.Instance != nil || d.ConfigPtr != nil {
		t.Fatal("catalog entry must exist with nil instance state")
	}
	inst, cfgPtr := d.Make()
	if built != 1 {
		t.Errorf("Make must construct exactly once: %d", built)
	}
	svc, isSvc := inst.(*catService)
	cfg, isCfg := cfgPtr.(*catCfg)
	if !isSvc || !isCfg || cfg != &svc.cfg {
		t.Errorf("Make must wire the accessor to the fresh instance")
	}
	if cfg.Level != "info" {
		t.Errorf("constructor defaults lost: %q", cfg.Level)
	}
	if d.Aliases[0] != "cat" || d.CfgType == nil {
		t.Errorf("declarations not recorded: %+v", d)
	}
}

func TestFreshInstancePerMake(t *testing.T) {
	reg, c := catalogWorld()
	built := 0
	chain("example.com/x/cat", &built).registerInto(reg, c)
	d, _ := reg.ByID("example.com/x/cat")
	a, _ := d.Make()
	b, _ := d.Make()
	if a == b || built != 2 {
		t.Error("each Make must construct fresh — this is the test-isolation guarantee")
	}
}

func TestCommitViolations(t *testing.T) {
	built := 0
	cases := []struct {
		name string
		reg  func() *Registration[catService]
		want string
	}{
		{"missing alias", func() *Registration[catService] {
			r := chain("example.com/x/cat", &built)
			r.aliases = nil
			return r
		}, "an alias is required"},
		{"bad alias charset", func() *Registration[catService] {
			return chain("example.com/x/cat", &built).Alias("Bad_Name")
		}, "lowercase letters, digits and hyphens"},
		{"reserved alias", func() *Registration[catService] {
			return chain("example.com/x/cat", &built).Alias("core")
		}, "reserved"},
		{"duplicate alias in chain", func() *Registration[catService] {
			return chain("example.com/x/cat", &built).Alias("cat")
		}, "declared twice"},
		{"bad id shape", func() *Registration[catService] {
			return chain("Example.com//x", &built)
		}, "path-shaped"},
		{"reserved id", func() *Registration[catService] {
			return chain(CoreID, &built)
		}, "reserved for the framework core"},
		{"provides not implemented", func() *Registration[catService] {
			return chain("example.com/x/cat", &built).Provides(Iface[Translator]())
		}, "does not implement"},
		{"non-interface token", func() *Registration[catService] {
			return chain("example.com/x/cat", &built).Provides(nil)
		}, "interface tokens"},
		{"visibility on non-applet", func() *Registration[catService] {
			return chain("example.com/x/cat", &built).Hidden()
		}, "apply only to applets"},
		{"nil accessor", func() *Registration[catService] {
			r := NewRegistration[catService, catCfg]("example.com/x/cat", func() *catService { return &catService{} }, nil)
			return r.Alias("cat")
		}, "nil config accessor"},
		{"metadata unknown key", func() *Registration[catService] {
			return chain("example.com/x/cat", &built).
				Metadata(&Metadata{Fields: map[string]any{"Nope": FieldMetadata[string]{}}})
		}, "names no config field"},
		{"metadata type mismatch", func() *Registration[catService] {
			return chain("example.com/x/cat", &built).
				Metadata(&Metadata{Fields: map[string]any{"Level": FieldMetadata[int]{Allowed: []int{1}}}})
		}, "allows int values"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg, c := catalogWorld()
			tc.reg().registerInto(reg, c)
			if c.Len() == 0 {
				t.Fatal("expected a commit violation")
			}
			joined := ""
			for _, err := range c.All() {
				joined += err.Error() + "\n"
			}
			if !strings.Contains(joined, tc.want) {
				t.Errorf("want %q in:\n%s", tc.want, joined)
			}
			if _, cataloged := reg.ByID("example.com/x/cat"); cataloged {
				t.Error("a violated registration must not enter the catalog")
			}
		})
	}
	if built != 0 {
		t.Errorf("no violation path may construct: %d constructions", built)
	}
}

func TestDuplicateIDAcrossCommits(t *testing.T) {
	reg, c := catalogWorld()
	built := 0
	chain("example.com/x/cat", &built).registerInto(reg, c)
	NewBareRegistration("example.com/x/cat", func() *catApplet { return &catApplet{} }).
		Alias("othercat").registerInto(reg, c)
	if c.Len() == 0 || !strings.Contains(c.All()[0].Error(), "duplicate id") {
		t.Errorf("two packages claiming one id must fail at the catalog: %v", c.All())
	}
}

func TestDoubleCommitOfOneChain(t *testing.T) {
	reg, c := catalogWorld()
	built := 0
	r := chain("example.com/x/cat", &built)
	r.registerInto(reg, c)
	r.registerInto(reg, c)
	if c.Len() == 0 || !strings.Contains(c.All()[0].Error(), "registered twice") {
		t.Errorf("double commit must be a violation: %v", c.All())
	}
}

func TestBareAndAppletCommit(t *testing.T) {
	reg, c := catalogWorld()
	NewBareRegistration("example.com/x/app", func() *catApplet { return &catApplet{} }).
		Alias("app", "a").Hidden().registerInto(reg, c)
	if c.Len() != 0 {
		t.Fatalf("unexpected violations: %v", c.All())
	}
	d, _ := reg.ByID("example.com/x/app")
	if !d.Hidden || d.CfgType != nil || len(d.Aliases) != 2 {
		t.Errorf("bare applet entry wrong: %+v", d)
	}
	inst, cfgPtr := d.Make()
	if _, isApplet := inst.(Applet); !isApplet || cfgPtr != nil {
		t.Error("bare Make must produce the applet and no config pointer")
	}
}

type badTagCfg struct {
	Version uint32 `json:"version"`
	X       string `conf:"x"` // no json tag: a registration-time violation
}

type badTagSvc struct{ cfg badTagCfg }

func TestCommitValidatesConfigTags(t *testing.T) {
	reg, c := catalogWorld()
	NewRegistration("example.com/x/badtag", func() *badTagSvc { return &badTagSvc{} },
		func(s *badTagSvc) *badTagCfg { return &s.cfg }).
		Alias("badtag").registerInto(reg, c)
	if c.Len() == 0 {
		t.Fatal("malformed config tags must be commit violations")
	}
	if !strings.Contains(c.All()[0].Error(), "json tag with a name is required") {
		t.Errorf("violation text wrong: %v", c.All())
	}
}
