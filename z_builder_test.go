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
	"strings"
	"testing"

	"sxcli.dev/fw/internal/registry"
)

type bldA struct{ cfg catCfg }
type bldB struct{ cfg catCfg }

func (a *bldA) Cat() {}
func (b *bldB) Cat() {}

// builderWorld catalogs two config-bearing services under a private
// catalog: a (alias alfa), b (alias bravo).
func builderWorld(t *testing.T) (*registry.Registry, func(*AppBuilder) (*App, error)) {
	t.Helper()
	reg, c := catalogWorld()
	NewRegistration("example.com/x/a", func() *bldA { return &bldA{cfg: catCfg{Level: "info"}} },
		func(s *bldA) *catCfg { return &s.cfg }).
		Alias("alfa").Provides(Iface[catIface]()).registerInto(reg, c)
	NewRegistration("example.com/x/b", func() *bldB { return &bldB{cfg: catCfg{Level: "info"}} },
		func(s *bldB) *catCfg { return &s.cfg }).
		Alias("bravo").Provides(Iface[catIface]()).registerInto(reg, c)
	if c.Len() != 0 {
		t.Fatalf("catalog setup failed: %v", c.All())
	}
	return reg, func(b *AppBuilder) (*App, error) { return b.buildFrom(reg, nil) }
}

func TestBuildComposesRankedOrder(t *testing.T) {
	_, build := builderWorld(t)
	app, err := build(Builder().AcceptAll().Order("example.com/x/b"))
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	all := app.reg.All()
	if len(all) != 2 || all[0].ID != "example.com/x/b" || all[1].ID != "example.com/x/a" {
		t.Fatalf("ranked must come first, rest by id: %v", ids2(app))
	}
	if all[0].Instance == nil || all[0].ConfigPtr == nil {
		t.Error("Build must instantiate accepted members")
	}
}

func TestCatalogStaysPristineAndAppsAreIndependent(t *testing.T) {
	cat, build := builderWorld(t)
	one, err1 := build(Builder().AcceptAll())
	two, err2 := build(Builder().AcceptAll())
	if err1 != nil || err2 != nil {
		t.Fatalf("build failed: %v %v", err1, err2)
	}
	d, _ := cat.ByID("example.com/x/a")
	if d.Instance != nil || d.ConfigPtr != nil {
		t.Error("the catalog entry must stay stateless after Build")
	}
	i1, _ := one.reg.ByID("example.com/x/a")
	i2, _ := two.reg.ByID("example.com/x/a")
	if i1.Instance == i2.Instance {
		t.Error("two Builds must not share instances")
	}
}

func TestAcceptIsASet(t *testing.T) {
	_, build := builderWorld(t)
	app, err := build(Builder().AcceptAll().Accept("example.com/x/a", "example.com/x/a"))
	if err != nil || len(app.reg.All()) != 2 {
		t.Errorf("admission is a set; overlap is harmless: %v", err)
	}
}

func TestAliasOverrideReplacesEntirely(t *testing.T) {
	_, build := builderWorld(t)
	app, err := build(Builder().AcceptAll().Alias("example.com/x/a", "anna", "aa"))
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	d, _ := app.reg.ByID("example.com/x/a")
	if strings.Join(d.Aliases, ",") != "anna,aa" {
		t.Errorf("Builder.Alias must replace the registration aliases: %v", d.Aliases)
	}
}

func TestCompositionViolations(t *testing.T) {
	cases := []struct {
		name string
		b    *AppBuilder
		want string
	}{
		{"unknown accept", Builder().Accept("example.com/x/ghost"), "unknown service id"},
		{"order without membership", Builder().Accept("example.com/x/a").Order("example.com/x/b"), "never admits"},
		{"order twice", Builder().AcceptAll().Order("example.com/x/a", "example.com/x/a"), "ranked twice"},
		{"alias without membership", Builder().Accept("example.com/x/a").Alias("example.com/x/b", "bb"), "not accepted"},
		{"alias reserved", Builder().AcceptAll().Alias("example.com/x/a", "core"), "reserved"},
		{"alias collision by rename", Builder().AcceptAll().Alias("example.com/x/a", "bravo"), "claimed by both"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, build := builderWorld(t)
			app, err := build(tc.b)
			if err == nil || app != nil {
				t.Fatal("expected a composition violation")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want %q in: %v", tc.want, err)
			}
		})
	}
}

func TestRegistrationAliasCollision(t *testing.T) {
	reg, c := catalogWorld()
	NewBareRegistration("example.com/x/one", func() *bldA { return &bldA{} }).
		Alias("same").registerInto(reg, c)
	NewBareRegistration("example.com/x/two", func() *bldB { return &bldB{} }).
		Alias("same").registerInto(reg, c)
	_, err := Builder().AcceptAll().buildFrom(reg, nil)
	if err == nil || !strings.Contains(err.Error(), `alias "same" is claimed by both`) {
		t.Errorf("colliding registration aliases must fail Build: %v", err)
	}
	// and the resolution is a composition rename
	app, err := Builder().AcceptAll().Alias("example.com/x/two", "other").buildFrom(reg, nil)
	if err != nil || app == nil {
		t.Errorf("Builder.Alias must resolve the collision: %v", err)
	}
}

func TestConcreteTypeAcceptedTwice(t *testing.T) {
	reg, c := catalogWorld()
	NewBareRegistration("example.com/x/one", func() *bldA { return &bldA{} }).
		Alias("one").registerInto(reg, c)
	NewBareRegistration("example.com/y/one", func() *bldA { return &bldA{} }).
		Alias("uno").registerInto(reg, c)
	if c.Len() != 0 {
		t.Fatalf("the catalog must tolerate same-type entries: %v", c.All())
	}
	if _, err := Builder().Accept("example.com/x/one").buildFrom(reg, nil); err != nil {
		t.Errorf("accepting one of them is fine: %v", err)
	}
	_, err := Builder().AcceptAll().buildFrom(reg, nil)
	if err == nil || !strings.Contains(err.Error(), "accepted as both") {
		t.Errorf("accepting both must fail Build: %v", err)
	}
}

func TestDefaultOutsideDomainFailsAtBuild(t *testing.T) {
	reg, c := catalogWorld()
	NewRegistration("example.com/x/bad", func() *catService {
		return &catService{cfg: catCfg{Level: "turbo"}} // not in the domain
	}, func(s *catService) *catCfg { return &s.cfg }).
		Alias("bad").
		Metadata(&Metadata{Fields: map[string]any{
			"Level": FieldMetadata[string]{Allowed: []string{"debug", "info"}},
		}}).registerInto(reg, c)
	if c.Len() != 0 {
		t.Fatalf("the commit is type-level only and must pass: %v", c.All())
	}
	_, err := Builder().AcceptAll().buildFrom(reg, nil)
	if err == nil || !strings.Contains(err.Error(), "not among the allowed values") {
		t.Errorf("the value-level check belongs to Build: %v", err)
	}
}

func TestOldStyleEntriesTolerated(t *testing.T) {
	reg, c := catalogWorld()
	// old path: instance registration, id doubles as operator name
	reg.Register("legacy", &bldA{}, registry.Options{})
	NewBareRegistration("example.com/x/two", func() *bldB { return &bldB{} }).
		Alias("bravo").registerInto(reg, c)
	app, err := Builder().AcceptAll().buildFrom(reg, nil)
	if err != nil || app == nil {
		t.Fatalf("coexistence Build must tolerate old-style entries: %v", err)
	}
	d, _ := app.reg.ByID("legacy")
	if d.Instance == nil {
		t.Error("old-style instance must ride through")
	}
}

func ids2(app *App) []string {
	var out []string
	for _, d := range app.reg.All() {
		out = append(out, d.ID)
	}
	return out
}
