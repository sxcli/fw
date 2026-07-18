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
	"fmt"
	"io"
	"io/fs"
	"strings"
	"testing"

	"sxcli.dev/fw/conf/engine"
	"sxcli.dev/fw/internal/fail"
	"sxcli.dev/fw/internal/registry"
)

// The composition end-to-end: catalog chains → Build → the pipeline,
// with alias-shaped operator surfaces.

type appCfg struct {
	Version  uint32 `json:"version"`
	Greeting string `json:"greeting" arg:"greeting,g" usage:"the greeting"`
}

type appSrv struct {
	cfg appCfg
	log *[]string
}

func (a *appSrv) Configured() error { return nil }
func (a *appSrv) Run() int {
	*a.log = append(*a.log, "srv.run:"+a.cfg.Greeting)
	return 0
}

type appAux struct{ log *[]string }

func (a *appAux) Configured() error {
	*a.log = append(*a.log, "aux.configured")
	return nil
}

// appWorld composes an App from a private catalog and wires a
// runtime with hermetic seams around it.
func appWorld(t *testing.T, b *AppBuilder, argv []string, files, env map[string]string, register func(reg *registry.Registry, c *fail.Collector, log *[]string)) (*world, int) {
	t.Helper()
	w := &world{c: &fail.Collector{}}
	reg, catalogC := catalogWorld()
	register(reg, catalogC, &w.log)
	app, err := b.buildFrom(reg, catalogC)
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	w.rt = &runtime{
		reg:  app.reg,
		c:    w.c,
		argv: argv,
		lookupEnv: func(name string) (string, bool) {
			v, ok := env[name]
			return v, ok
		},
		stdout: &w.stdout,
		stderr: &w.stderr,
		locations: func(alias string) []engine.Location {
			return []engine.Location{{Base: "/etc/" + alias + "/config"}}
		},
		stat: func(path string) (int64, error) {
			var size int64
			err := fs.ErrNotExist
			if content, ok := files[path]; ok {
				size = int64(len(content))
				err = nil
			}
			return size, err
		},
		open: func(path string) (io.ReadCloser, error) {
			var r io.ReadCloser
			err := fs.ErrNotExist
			if content, ok := files[path]; ok {
				r = io.NopCloser(strings.NewReader(content))
				err = nil
			}
			return r, err
		},
		openPinned: func(path string) (io.ReadCloser, error) { return nil, fs.ErrNotExist },
	}
	t.Cleanup(func() { positionals = nil; activeTranslator = nil })
	// the app is already built above; drive the pipeline directly
	return w, run(w.rt)
}

func registerSrv(alias ...string) func(reg *registry.Registry, c *fail.Collector, log *[]string) {
	return func(reg *registry.Registry, c *fail.Collector, log *[]string) {
		NewRegistration("example.com/app/srv", func() *appSrv { return &appSrv{log: log, cfg: appCfg{Version: 1, Greeting: "default"}} },
			func(s *appSrv) *appCfg { return &s.cfg }).
			Alias(alias...).registerInto(reg, c)
	}
}

func TestAppAliasSurfaces(t *testing.T) {
	// args
	w, code := appWorld(t, Builder().AcceptAll(), []string{"bin", "--greeting=hi"}, nil, nil, registerSrv("srv"))
	if code != 0 || strings.Join(w.log, ",") != "srv.run:hi" {
		t.Errorf("arg surface wrong: code=%d log=%v stderr:\n%s", code, w.log, w.stderr.String())
	}
	// env: prefix from the ALIAS
	w, code = appWorld(t, Builder().AcceptAll(), []string{"bin"}, nil,
		map[string]string{"SRV_GREETING": "fromenv"}, registerSrv("srv"))
	if code != 0 || strings.Join(w.log, ",") != "srv.run:fromenv" {
		t.Errorf("env surface wrong: code=%d log=%v", code, w.log)
	}
	// config file: location AND section from the alias
	files := map[string]string{"/etc/srv/config.json": `{"srv": {"greeting": "fromfile"}}`}
	w, code = appWorld(t, Builder().AcceptAll(), []string{"bin"}, files, nil, registerSrv("srv"))
	if code != 0 || strings.Join(w.log, ",") != "srv.run:fromfile" {
		t.Errorf("file surface wrong: code=%d log=%v stderr:\n%s", code, w.log, w.stderr.String())
	}
}

func TestHyphenAliasReachesEnv(t *testing.T) {
	w, code := appWorld(t, Builder().AcceptAll(), []string{"bin"}, nil,
		map[string]string{"CHERRY_PICK_GREETING": "picked"}, registerSrv("cherry-pick"))
	if code != 0 || strings.Join(w.log, ",") != "srv.run:picked" {
		t.Errorf("hyphen alias env mapping wrong: code=%d log=%v stderr:\n%s", code, w.log, w.stderr.String())
	}
}

func TestSecondaryAliasSelects(t *testing.T) {
	w, code := appWorld(t, Builder().AcceptAll(), []string{"bin", "cp", "--greeting=via-cp"}, nil, nil,
		func(reg *registry.Registry, c *fail.Collector, log *[]string) {
			registerSrv("cherry-pick", "cp")(reg, c, log)
			NewBareRegistration("example.com/app/two", func() *appSrv2 { return &appSrv2{log: log} }).
				Alias("two").registerInto(reg, c)
		})
	if code != 0 || strings.Join(w.log, ",") != "srv.run:via-cp" {
		t.Errorf("secondary alias must select: code=%d log=%v stderr:\n%s", code, w.log, w.stderr.String())
	}
}

type appSrv2 struct{ log *[]string }

func (a *appSrv2) Configured() error { return nil }
func (a *appSrv2) Run() int          { *a.log = append(*a.log, "two.run"); return 0 }

func TestUsageListsPrimariesInRankOrder(t *testing.T) {
	b := Builder().AcceptAll().Order("example.com/app/two", "example.com/app/srv")
	w, code := appWorld(t, b, []string{"bin", "ghost"}, nil, nil,
		func(reg *registry.Registry, c *fail.Collector, log *[]string) {
			registerSrv("cherry-pick", "cp")(reg, c, log)
			NewBareRegistration("example.com/app/two", func() *appSrv2 { return &appSrv2{log: log} }).
				Alias("two").registerInto(reg, c)
		})
	if code != 2 {
		t.Fatalf("dispatch failure expected, code=%d", code)
	}
	text := w.stderr.String()
	if !strings.Contains(text, "two") || !strings.Contains(text, "cherry-pick") || strings.Contains(text, "cp\n") {
		t.Errorf("usage must list primaries: %s", text)
	}
	if strings.Index(text, "two") > strings.Index(text, "cherry-pick") {
		t.Errorf("usage must follow rank order: %s", text)
	}
}

func TestControlsSpeakBothVocabularies(t *testing.T) {
	byAlias := func(reg *registry.Registry, c *fail.Collector, log *[]string) {
		registerSrv("srv")(reg, c, log)
		NewBareRegistration("example.com/app/aux", func() *appAux { return &appAux{log: log} }).
			Alias("aux").registerInto(reg, c)
	}
	w, code := appWorld(t, Builder().AcceptAll(), []string{"bin", "--enable", "aux"}, nil, nil, byAlias)
	if code != 0 || !strings.Contains(strings.Join(w.log, ","), "aux.configured") {
		t.Errorf("enable by alias failed: code=%d log=%v", code, w.log)
	}
	w, code = appWorld(t, Builder().AcceptAll(), []string{"bin", "--enable", "example.com/app/aux"}, nil, nil, byAlias)
	if code != 0 || !strings.Contains(strings.Join(w.log, ","), "aux.configured") {
		t.Errorf("enable by id failed: code=%d log=%v", code, w.log)
	}
	w, code = appWorld(t, Builder().AcceptAll(), []string{"bin", "--enable", "ghost"}, nil, nil, byAlias)
	if code != 2 || !strings.Contains(w.stderr.String(), `enable: unknown service "ghost"`) {
		t.Errorf("unknown ref must fail with the operator vocabulary: code=%d\n%s", code, w.stderr.String())
	}
}

func TestBuildSurfacesCommitViolations(t *testing.T) {
	reg, catalogC := catalogWorld()
	NewBareRegistration("example.com/app/bad", func() *appAux { return &appAux{} }).
		registerInto(reg, catalogC) // no alias: a commit violation
	_, err := Builder().AcceptAll().buildFrom(reg, catalogC)
	if err == nil || !strings.Contains(err.Error(), "an alias is required") {
		t.Errorf("Build must surface commit violations: %v", err)
	}
}

func TestAmbiguityResolvedByOrderEndToEnd(t *testing.T) {
	register := func(reg *registry.Registry, c *fail.Collector, log *[]string) {
		NewBareRegistration("example.com/app/consumer", func() *catConsumer { return &catConsumer{log: log} }).
			Alias("consumer").registerInto(reg, c)
		NewBareRegistration("example.com/app/one", func() *bldA { return &bldA{} }).
			Alias("one").Provides(Iface[catIface]()).registerInto(reg, c)
		NewBareRegistration("example.com/app/two", func() *bldB { return &bldB{} }).
			Alias("two").Provides(Iface[catIface]()).registerInto(reg, c)
	}
	// unranked tie → startup violation pointing at the vet tool
	w, code := appWorld(t, Builder().AcceptAll(), []string{"bin"}, nil, nil, register)
	if code != 2 || !strings.Contains(w.stderr.String(), "sxclivet") {
		t.Errorf("unranked tie must refuse to start with the vet nudge: code=%d\n%s", code, w.stderr.String())
	}
	// Order resolves it — the ranked provider wins
	w, code = appWorld(t, Builder().AcceptAll().Order("example.com/app/two"), []string{"bin"}, nil, nil, register)
	if code != 0 || strings.Join(w.log, ",") != "consumer.run:*fw.bldB" {
		t.Errorf("rank must choose: code=%d log=%v stderr:\n%s", code, w.log, w.stderr.String())
	}
}

type catConsumer struct {
	Dep catIface `inject:""`
	log *[]string
}

func (a *catConsumer) Configured() error { return nil }
func (a *catConsumer) Run() int {
	*a.log = append(*a.log, fmt.Sprintf("consumer.run:%T", a.Dep))
	return 0
}

func TestOrderSequenceDecidesAmongRanked(t *testing.T) {
	register := func(reg *registry.Registry, c *fail.Collector, log *[]string) {
		NewBareRegistration("example.com/app/consumer", func() *catConsumer { return &catConsumer{log: log} }).
			Alias("consumer").registerInto(reg, c)
		NewBareRegistration("example.com/app/one", func() *bldA { return &bldA{} }).
			Alias("one").Provides(Iface[catIface]()).registerInto(reg, c)
		NewBareRegistration("example.com/app/two", func() *bldB { return &bldB{} }).
			Alias("two").Provides(Iface[catIface]()).registerInto(reg, c)
		NewBareRegistration("example.com/app/three", func() *catService { return &catService{} }).
			Alias("three").Provides(Iface[catIface]()).registerInto(reg, c)
	}
	// two of three ranked: the Order-earlier one wins the field
	w, code := appWorld(t, Builder().AcceptAll().
		Order("example.com/app/two", "example.com/app/one"), []string{"bin"}, nil, nil, register)
	if code != 0 || strings.Join(w.log, ",") != "consumer.run:*fw.bldB" {
		t.Errorf("Order-earlier must win: code=%d log=%v stderr:\n%s", code, w.log, w.stderr.String())
	}
	// flipping the sequence flips the winner — the sequence IS the semantics
	w, code = appWorld(t, Builder().AcceptAll().
		Order("example.com/app/one", "example.com/app/two"), []string{"bin"}, nil, nil, register)
	if code != 0 || strings.Join(w.log, ",") != "consumer.run:*fw.bldA" {
		t.Errorf("flipped Order must flip the winner: code=%d log=%v", code, w.log)
	}
}

// appProbe runs assertions against the injected Introspector from
// inside the composed world, keeping the whole registry alive.
type appProbe struct {
	I  *Introspector `inject:""`
	do func(i *Introspector)
}

func (p *appProbe) Configured() error { return nil }
func (p *appProbe) Run() int          { p.do(p.I); return 0 }

func TestIntrospectionSpeaksAliases(t *testing.T) {
	var applets, services []string
	var single, descByAlias, descByID string
	var argsByAlias, argsByID, argsUnknown error
	register := func(reg *registry.Registry, c *fail.Collector, log *[]string) {
		NewBareRegistration("example.com/app/probe", func() *appProbe {
			return &appProbe{do: func(i *Introspector) {
				applets = i.Applets()
				services = i.Services()
				single, _ = i.SingleApplet()
				descByAlias = i.Describe("described")
				descByID = i.Describe("example.com/app/described")
				_, argsByAlias = i.Arguments("probe", nil)
				_, argsByID = i.Arguments("example.com/app/probe", nil)
				_, argsUnknown = i.Arguments("ghost", nil)
			}}
		}).Alias("probe").registerInto(reg, c)
		NewBareRegistration("example.com/app/described", func() *appAux { return &appAux{log: log} }).
			Alias("described").Metadata(&Metadata{Description: "a well-described service"}).
			registerInto(reg, c)
	}
	w, code := appWorld(t, Builder().AcceptAll(), []string{"bin"}, nil, nil, register)
	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
	if strings.Join(applets, ",") != "probe" {
		t.Errorf("Applets must speak aliases: %v", applets)
	}
	if single != "probe" {
		t.Errorf("SingleApplet must speak the alias: %q", single)
	}
	joined := strings.Join(services, ",")
	if services[0] != "core" || !strings.Contains(joined, "described") || !strings.Contains(joined, "introspection") || strings.Contains(joined, "example.com") {
		t.Errorf("Services must be operator names, core first, introspection included: %v", services)
	}
	if descByAlias != "a well-described service" || descByID != descByAlias {
		t.Errorf("Describe must accept both vocabularies: %q / %q", descByAlias, descByID)
	}
	if argsByAlias != nil || argsByID != nil {
		t.Errorf("Arguments must accept both vocabularies: %v / %v", argsByAlias, argsByID)
	}
	if argsUnknown == nil {
		t.Error("Arguments must reject unknown references")
	}
}
