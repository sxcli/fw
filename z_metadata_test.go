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
	"reflect"
	"strings"
	"sxcli.dev/fw/system"
	"testing"
	"time"
)

// metaProbe introspects its OWN target view; the extra dependency
// makes test/extra a member of the resolved service set — the
// target-scoped way to see an
// annotated service (controls in Arguments args are inert by design).
type metaProbe struct {
	Sys   system.System `inject:""`
	Extra *extraService `inject:"test/extra"`
	do    func(sys system.System)
}

func (p *metaProbe) Configured() error { return nil }
func (p *metaProbe) Run() int          { p.do(p.Sys); return 0 }

func annotatedWorld(t *testing.T, md *Metadata) (*world, *extraService) {
	t.Helper()
	w := newWorld(t, []string{"bin", "meta"}, nil, nil)
	w.applet(0)
	extra := &extraService{cfg: extraCfg{Version: 1, Flag: "fast"}}
	NewRegistration("test/extra", func() *extraService { return extra },
		func(x *extraService) *extraCfg { return &x.cfg }).
		Alias("extra").Metadata(md).registerInto(w.cat, w.c)
	return w, extra
}

func TestMetadataFlowsIntoIntrospection(t *testing.T) {
	md := &Metadata{
		Description: "an example service with a closed flag domain",
		Fields: map[string]any{
			"Flag": FieldMetadata[string]{Allowed: []string{"fast", "slow"}, Doc: "the pace of things"},
		},
	}
	var desc string
	var infos []ArgInfo
	w, _ := annotatedWorld(t, md)
	probe := &metaProbe{do: func(sys system.System) {
		view := sys.Introspector("meta")
		desc = view.Describe("extra")
		infos = view.Arguments(nil)
	}}
	NewBareRegistration("test/meta", func() *metaProbe { return probe }).
		Alias("meta").registerInto(w.cat, w.c)
	if code := w.run(); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
	if desc != "an example service with a closed flag domain" {
		t.Errorf("Describe wrong: %q", desc)
	}
	found := false
	for _, a := range infos {
		if a.Long == "extra-flag" {
			found = true
			if fmt.Sprint(a.Allowed) != "[fast slow]" || a.Doc != "the pace of things" {
				t.Errorf("annotation not exposed: %+v", a)
			}
		}
	}
	if !found {
		t.Fatalf("extra-flag not in schema: %v", infos)
	}
}

func TestMetadataViolations(t *testing.T) {
	cases := []struct {
		name string
		md   *Metadata
		want string
	}{
		{"unknown field", &Metadata{Fields: map[string]any{"Nope": FieldMetadata[string]{}}}, "names no field of the config struct"},
		{"wrong value type", &Metadata{Fields: map[string]any{"Flag": "not field metadata"}}, "it must be a FieldMetadata"},
		{"type mismatch", &Metadata{Fields: map[string]any{"Flag": FieldMetadata[int]{Allowed: []int{1, 2}}}}, "has Allowed values of type int"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := annotatedWorld(t, tc.md)
			if w.c.Len() == 0 {
				t.Fatal("expected a registration violation")
			}
			joined := ""
			for _, err := range w.c.All() {
				joined += err.Error() + "\n"
			}
			if !strings.Contains(joined, tc.want) {
				t.Errorf("want %q in:\n%s", tc.want, joined)
			}
		})
	}
}

func TestMetadataFieldsWithoutConfigIsViolation(t *testing.T) {
	w := newWorld(t, []string{"bin"}, nil, nil)
	w.applet(0)
	NewBareRegistration("test/bare", func() *plainService { return &plainService{} }).
		Alias("bare").
		Metadata(&Metadata{Fields: map[string]any{"X": FieldMetadata[string]{}}}).
		registerInto(w.cat, w.c)
	if w.c.Len() == 0 {
		t.Fatal("field metadata without a config struct must be a violation")
	}
}

func TestDescriptionAloneWithoutConfigIsFine(t *testing.T) {
	w := newWorld(t, []string{"bin"}, nil, nil)
	w.applet(0)
	NewBareRegistration("test/bare", func() *plainService { return &plainService{} }).
		Alias("bare").
		Metadata(&Metadata{Description: "a config-less but well-described service"}).
		registerInto(w.cat, w.c)
	if w.c.Len() != 0 {
		t.Fatalf("description-only metadata must be fine: %v", w.c.All())
	}
}

func enforcementWorld(t *testing.T, argv []string, files, env map[string]string) (*world, *extraService) {
	t.Helper()
	w := newWorld(t, argv, files, env)
	w.applet(0)
	extra := &extraService{cfg: extraCfg{Version: 1, Flag: "fast"}}
	NewRegistration("test/extra", func() *extraService { return extra },
		func(x *extraService) *extraCfg { return &x.cfg }).
		Alias("extra").
		Metadata(&Metadata{Fields: map[string]any{
			"Flag": FieldMetadata[string]{Allowed: []string{"fast", "slow"}},
			"Tags": FieldMetadata[string]{Allowed: []string{"a", "b"}},
		}}).registerInto(w.cat, w.c)
	return w, extra
}

func TestDomainEnforcedOnArguments(t *testing.T) {
	w, _ := enforcementWorld(t, []string{"bin", "--enable", "extra", "--extra-flag", "turbo"}, nil, nil)
	if code := w.run(); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(w.stderr.String(), "not among the allowed values") {
		t.Errorf("domain violation not reported:\n%s", w.stderr.String())
	}
	w2, extra := enforcementWorld(t, []string{"bin", "--enable", "extra", "--extra-flag", "slow"}, nil, nil)
	if code := w2.run(); code != 0 {
		t.Fatalf("in-domain value must pass: exit %d\n%s", code, w2.stderr.String())
	}
	if extra.cfg.Flag != "slow" {
		t.Errorf("value not applied: %q", extra.cfg.Flag)
	}
}

func TestDomainEnforcedOnEnvironment(t *testing.T) {
	w, _ := enforcementWorld(t, []string{"bin", "--enable", "extra"}, nil, map[string]string{"APP__EXTRA_FLAG": "turbo"})
	if code := w.run(); code != 2 {
		t.Fatalf("exit = %d, want 2\n%s", code, w.stderr.String())
	}
	if !strings.Contains(w.stderr.String(), "$APP__EXTRA_FLAG") {
		t.Errorf("violation must name the env source:\n%s", w.stderr.String())
	}
}

func TestDomainEnforcedOnFiles(t *testing.T) {
	files := map[string]string{"/etc/app/config.json": `{"extra": {"flag": "turbo"}}`}
	w, _ := enforcementWorld(t, []string{"bin", "--enable", "extra"}, files, nil)
	if code := w.run(); code != 2 {
		t.Fatalf("exit = %d, want 2\n%s", code, w.stderr.String())
	}
	if !strings.Contains(w.stderr.String(), "config extra.flag") {
		t.Errorf("violation must name the file source:\n%s", w.stderr.String())
	}
}

func TestSliceDomainEnforced(t *testing.T) {
	w, _ := enforcementWorld(t, []string{"bin", "--enable", "extra", "--extra-tag", "a", "--extra-tag", "z"}, nil, nil)
	if code := w.run(); code != 2 {
		t.Fatalf("bad slice element must fail: exit %d", code)
	}
	w2, extra := enforcementWorld(t, []string{"bin", "--enable", "extra", "--extra-tag", "a", "--extra-tag", "b"}, nil, nil)
	if code := w2.run(); code != 0 {
		t.Fatalf("in-domain elements must pass: exit %d\n%s", code, w2.stderr.String())
	}
	if strings.Join(extra.cfg.Tags, ",") != "a,b" {
		t.Errorf("values not applied: %v", extra.cfg.Tags)
	}
}

// Ledger note: the default-in-domain check is value-level and moved
// from registration to Build with the two-phase split — instances
// exist only there. The z_builder suite covers the Build side; this
// proves the end-to-end refusal.
func TestDefaultOutsideDomainFailsTheRun(t *testing.T) {
	w := newWorld(t, []string{"bin"}, nil, nil)
	w.applet(0)
	NewRegistration("test/extra", func() *extraService {
		return &extraService{cfg: extraCfg{Version: 1, Flag: "turbo"}} // default not in the domain
	}, func(x *extraService) *extraCfg { return &x.cfg }).
		Alias("extra").
		Metadata(&Metadata{Fields: map[string]any{
			"Flag": FieldMetadata[string]{Allowed: []string{"fast", "slow"}},
		}}).registerInto(w.cat, w.c)
	if w.c.Len() != 0 {
		t.Fatalf("the commit is type-level and must pass: %v", w.c.All())
	}
	if code := w.run(); code != 2 || !strings.Contains(w.stderr.String(), "has an invalid default value") {
		t.Errorf("the run must refuse the bad default: code=%d\n%s", code, w.stderr.String())
	}
}

func TestSliceDomainEnforcedFromFiles(t *testing.T) {
	files := map[string]string{"/etc/app/config.json": `{"extra": {"tags": ["a", "z"]}}`}
	w, _ := enforcementWorld(t, []string{"bin", "--enable", "extra"}, files, nil)
	if code := w.run(); code != 2 {
		t.Fatalf("exit = %d, want 2\n%s", code, w.stderr.String())
	}
	if !strings.Contains(w.stderr.String(), "config extra.tags") {
		t.Errorf("violation must name the file source:\n%s", w.stderr.String())
	}
}

func TestSliceDomainEnforcedFromEnvironment(t *testing.T) {
	w, _ := enforcementWorld(t, []string{"bin", "--enable", "extra"}, nil, map[string]string{"APP__EXTRA_TAG": "a,z"})
	if code := w.run(); code != 2 {
		t.Fatalf("exit = %d, want 2\n%s", code, w.stderr.String())
	}
	if !strings.Contains(w.stderr.String(), "$APP__EXTRA_TAG") {
		t.Errorf("violation must name the env source:\n%s", w.stderr.String())
	}
}

func TestSliceDefaultOutsideDomainFailsTheRun(t *testing.T) {
	w := newWorld(t, []string{"bin"}, nil, nil)
	w.applet(0)
	extra := &extraService{cfg: extraCfg{Version: 1, Flag: "fast", Tags: []string{"a", "zz"}}}
	NewRegistration("test/extra", func() *extraService { return extra },
		func(x *extraService) *extraCfg { return &x.cfg }).
		Alias("extra").
		Metadata(&Metadata{Fields: map[string]any{
			"Flag": FieldMetadata[string]{Allowed: []string{"fast", "slow"}},
			"Tags": FieldMetadata[string]{Allowed: []string{"a", "b"}},
		}}).registerInto(w.cat, w.c)
	if w.c.Len() != 0 {
		t.Fatalf("the commit is type-level and must pass: %v", w.c.All())
	}
	// the value-level default check belongs to Build
	if code := w.run(); code != 2 || !strings.Contains(w.stderr.String(), "It can contain only") {
		t.Errorf("the run must name the offending default element: code=%d\n%s", code, w.stderr.String())
	}
}

func TestNilAndAbsentMetadataAreHarmless(t *testing.T) {
	// regression: registerOptions.metadata is a typed-nil *Metadata for
	// every metadata-less registration; the check must not treat it as
	// present (this panicked once, in the yaml provider's init)
	w := newWorld(t, []string{"bin"}, nil, nil)
	w.applet(0)
	NewRegistration("test/plainmeta", func() *extraService { return &extraService{cfg: extraCfg{Version: 1, Flag: "fast"}} },
		func(x *extraService) *extraCfg { return &x.cfg }).
		Alias("plainmeta").Metadata(nil).registerInto(w.cat, w.c)
	if w.c.Len() != 0 {
		t.Fatalf("nil metadata must be treated as absent: %v", w.c.All())
	}
	if code := w.run(); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
}

func TestDescribeEdgeCases(t *testing.T) {
	var unknown, unannotated string
	w := newWorld(t, []string{"bin", "meta"}, nil, nil)
	w.applet(0)
	w.dep(false) // registered, no metadata
	probe := &argsProbe{do: func(sys system.System) {
		view := sys.Introspector("meta")
		unknown = view.Describe("nope")
		unannotated = view.Describe("dep") // registered, but outside the resolved service set
	}}
	NewBareRegistration("test/meta", func() *argsProbe { return probe }).
		Alias("meta").registerInto(w.cat, w.c)
	if code := w.run(); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
	if unknown != "" || unannotated != "" {
		t.Errorf("Describe must return empty for unknown/unannotated: %q, %q", unknown, unannotated)
	}
}

// intService proves non-string domains end to end.
type intCfg struct {
	Version uint32 `json:"version"`
	Retries int    `json:"retries" conf:"retries-x" usage:"attempts"`
}

type intService struct {
	cfg intCfg
}

func intWorld(t *testing.T, argv []string) (*world, *intService) {
	t.Helper()
	w := newWorld(t, argv, nil, nil)
	w.applet(0)
	svc := &intService{cfg: intCfg{Version: 1, Retries: 1}}
	NewRegistration("test/intsvc", func() *intService { return svc },
		func(x *intService) *intCfg { return &x.cfg }).
		Alias("intsvc").
		Metadata(&Metadata{Fields: map[string]any{
			"Retries": FieldMetadata[int]{Allowed: []int{1, 3, 5}},
		}}).registerInto(w.cat, w.c)
	return w, svc
}

func TestIntDomainEnforcedEndToEnd(t *testing.T) {
	w, _ := intWorld(t, []string{"bin", "--enable", "intsvc", "--retries-x", "7"})
	if code := w.run(); code != 2 {
		t.Fatalf("out-of-domain int must fail: exit %d\n%s", code, w.stderr.String())
	}
	w2, svc := intWorld(t, []string{"bin", "--enable", "intsvc", "--retries-x", "3"})
	if code := w2.run(); code != 0 {
		t.Fatalf("in-domain int must pass: exit %d\n%s", code, w2.stderr.String())
	}
	if svc.cfg.Retries != 3 {
		t.Errorf("value not applied: %d", svc.cfg.Retries)
	}
}

func TestArgInfoSliceTypeIsElementType(t *testing.T) {
	var tagInfo *ArgInfo
	w, _ := enforcementWorld(t, []string{"bin", "meta"}, nil, nil)
	probe := &metaProbe{do: func(sys system.System) {
		infos := sys.Introspector("meta").Arguments(nil)
		for idx := range infos {
			if infos[idx].Long == "extra-tag" {
				tagInfo = &infos[idx]
			}
		}
	}}
	NewBareRegistration("test/meta", func() *metaProbe { return probe }).
		Alias("meta").registerInto(w.cat, w.c)
	if code := w.run(); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
	if tagInfo == nil {
		t.Fatal("extra-tag not found in schema")
	}
	if !tagInfo.IsSlice || tagInfo.Type != reflect.TypeOf("") {
		t.Errorf("slice ArgInfo must carry the element type: IsSlice=%v Type=%v", tagInfo.IsSlice, tagInfo.Type)
	}
	if fmt.Sprint(tagInfo.Allowed) != "[a b]" {
		t.Errorf("slice domain must be exposed: %v", tagInfo.Allowed)
	}
}

func TestNamedTypeAllowedValuesConvert(t *testing.T) {
	type pace string
	// []pace on a string field: same kind, convertible — legal
	w, _ := annotatedWorld(t, &Metadata{Fields: map[string]any{
		"Flag": FieldMetadata[pace]{Allowed: []pace{"fast", "slow"}},
	}})
	if w.c.Len() != 0 {
		t.Fatalf("same-kind convertible allowed values must be accepted: %v", w.c.All())
	}
}

func TestHintViolations(t *testing.T) {
	cases := []struct {
		name string
		md   *Metadata
		want string
	}{
		{"hint plus allowed", &Metadata{Fields: map[string]any{
			"Flag": FieldMetadata[string]{Allowed: []string{"fast", "slow"}, Hint: HintFile},
		}}, "has both Allowed and Hint"},
		{"unknown hint", &Metadata{Fields: map[string]any{
			"Flag": FieldMetadata[string]{Hint: ValueHint(42)},
		}}, "unknown Hint"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := annotatedWorld(t, tc.md)
			if w.c.Len() == 0 {
				t.Fatal("expected a registration violation")
			}
			joined := ""
			for _, err := range w.c.All() {
				joined += err.Error() + "\n"
			}
			if !strings.Contains(joined, tc.want) {
				t.Errorf("want %q in:\n%s", tc.want, joined)
			}
		})
	}
}

func TestHintOnNonStringFieldIsViolation(t *testing.T) {
	w := newWorld(t, []string{"bin"}, nil, nil)
	w.applet(0)
	NewRegistration("test/intsvc", func() *intService { return &intService{cfg: intCfg{Version: 1, Retries: 1}} },
		func(x *intService) *intCfg { return &x.cfg }).
		Alias("intsvc").
		Metadata(&Metadata{Fields: map[string]any{
			"Retries": FieldMetadata[int]{Hint: HintFile},
		}}).registerInto(w.cat, w.c)
	if w.c.Len() == 0 {
		t.Fatal("hint on an int field must be a violation")
	}
	if !strings.Contains(w.c.All()[0].Error(), "is not a string") {
		t.Errorf("violation text wrong: %v", w.c.All())
	}
}

// The hint travels to ArgInfo, and the core dogfoods the mechanism:
// its own --config declares HintFile.
func TestHintFlowsIntoIntrospection(t *testing.T) {
	md := &Metadata{Fields: map[string]any{
		"Flag": FieldMetadata[string]{Hint: HintFile, Doc: "some path"},
	}}
	var flagHint, configHint, disableHint ValueHint
	w, _ := annotatedWorld(t, md)
	probe := &metaProbe{do: func(sys system.System) {
		infos := sys.Introspector("meta").Arguments(nil)
		for _, a := range infos {
			if a.Long == "extra-flag" {
				flagHint = a.Hint
			}
			if a.Service == "core" && a.Long == "config" {
				configHint = a.Hint
			}
			if a.Service == "core" && a.Long == "disable" {
				disableHint = a.Hint
			}
		}
	}}
	NewBareRegistration("test/meta", func() *metaProbe { return probe }).
		Alias("meta").registerInto(w.cat, w.c)
	if code := w.run(); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, w.stderr.String())
	}
	if flagHint != HintFile {
		t.Errorf("service field hint not exposed: %v", flagHint)
	}
	if configHint != HintFile {
		t.Errorf("core --config must declare HintFile: %v", configHint)
	}
	if disableHint != HintServiceID {
		t.Errorf("core --disable must declare HintServiceID: %v", disableHint)
	}
}

type boundCfg struct {
	Version uint32        `json:"version"`
	Port    int           `json:"port" conf:"port-x" usage:"the port"`
	Wait    time.Duration `json:"wait" conf:"wait-x" usage:"the wait"`
	Sizes   []int         `json:"sizes" conf:"size-x" usage:"pool sizes"`
	Debug   bool          `json:"debug"`
}

type boundService struct {
	cfg boundCfg
}

func boundWorld(t *testing.T, argv []string, files, env map[string]string, md *Metadata, cfg boundCfg) (*world, *boundService) {
	t.Helper()
	w := newWorld(t, argv, files, env)
	w.applet(0)
	svc := &boundService{cfg: cfg}
	NewRegistration("test/bound", func() *boundService { return svc },
		func(x *boundService) *boundCfg { return &x.cfg }).
		Alias("bound").Metadata(md).registerInto(w.cat, w.c)
	return w, svc
}

func portBounds() *Metadata {
	return &Metadata{Fields: map[string]any{
		"Port":  FieldMetadata[int]{Min: Bound(1), Max: Bound(65535)},
		"Wait":  FieldMetadata[time.Duration]{Min: Bound(time.Second)},
		"Sizes": FieldMetadata[int]{Min: Bound(1)},
	}}
}

func TestBoundsEnforcedEndToEnd(t *testing.T) {
	good := boundCfg{Version: 1, Port: 80, Wait: time.Minute}
	cases := []struct {
		name  string
		argv  []string
		files map[string]string
		env   map[string]string
		want  string
	}{
		{"argument above maximum", []string{"bin", "--enable", "bound", "--port-x", "70000"}, nil, nil, "--port-x: the value 70000 is above the maximum 65535"},
		{"environment below minimum", []string{"bin", "--enable", "bound"}, nil, map[string]string{"APP__PORT_X": "0"}, "$APP__PORT_X: the value 0 is below the minimum 1"},
		{"file below minimum", []string{"bin", "--enable", "bound"}, map[string]string{"/etc/app/config.json": `{"bound": {"port": 0}}`}, nil, "config bound.port: the value 0 is below the minimum 1"},
		{"duration below minimum", []string{"bin", "--enable", "bound", "--wait-x", "10ms"}, nil, nil, "--wait-x: the value 10ms is below the minimum 1s"},
		{"slice element below minimum", []string{"bin", "--enable", "bound", "--size-x", "0"}, nil, nil, "--size-x: the value 0 is below the minimum 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := boundWorld(t, tc.argv, tc.files, tc.env, portBounds(), good)
			if code := w.run(); code != 2 || !strings.Contains(w.stderr.String(), tc.want) {
				t.Errorf("want exit 2 with %q, got %d:\n%s", tc.want, code, w.stderr.String())
			}
		})
	}
	w, svc := boundWorld(t, []string{"bin", "--enable", "bound", "--port-x", "65535", "--wait-x", "1s"}, nil, nil, portBounds(), good)
	if code := w.run(); code != 0 {
		t.Fatalf("values on the bounds must pass: exit %d\n%s", code, w.stderr.String())
	}
	if svc.cfg.Port != 65535 || svc.cfg.Wait != time.Second {
		t.Errorf("values not applied: %d %v", svc.cfg.Port, svc.cfg.Wait)
	}
}

func TestDefaultOutsideBoundsFailsTheRun(t *testing.T) {
	cases := []struct {
		name string
		cfg  boundCfg
		want string
	}{
		{"below minimum", boundCfg{Version: 1, Port: 0, Wait: time.Minute}, `field "Port" has an invalid default value: 0. It must be at least 1`},
		{"above maximum", boundCfg{Version: 1, Port: 70000, Wait: time.Minute}, `field "Port" has an invalid default value: 70000. It must be at most 65535`},
		{"slice element below minimum", boundCfg{Version: 1, Port: 80, Wait: time.Minute, Sizes: []int{2, 0}}, `field "Sizes" has an invalid default value: 0. It must be at least 1`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := boundWorld(t, []string{"bin"}, nil, nil, portBounds(), tc.cfg)
			if w.c.Len() != 0 {
				t.Fatalf("the commit is type-level and must pass: %v", w.c.All())
			}
			if code := w.run(); code != 2 || !strings.Contains(w.stderr.String(), tc.want) {
				t.Errorf("want exit 2 with %q, got %d:\n%s", tc.want, code, w.stderr.String())
			}
		})
	}
}

func TestBoundViolationsAtRegistration(t *testing.T) {
	cases := []struct {
		name string
		md   *Metadata
		want string
	}{
		{"min above max", &Metadata{Fields: map[string]any{"Port": FieldMetadata[int]{Min: Bound(10), Max: Bound(1)}}}, `field "Port" has Min 10 greater than its Max 1`},
		{"allowed and bounds", &Metadata{Fields: map[string]any{"Port": FieldMetadata[int]{Allowed: []int{80}, Max: Bound(1024)}}}, `field "Port" has both Allowed and Min/Max`},
		{"wrong type", &Metadata{Fields: map[string]any{"Port": FieldMetadata[string]{Min: Bound("a")}}}, `field "Port" has Min/Max of type string but the field's type is int`},
		{"bool", &Metadata{Fields: map[string]any{"Debug": FieldMetadata[bool]{Min: new(bool)}}}, `field "Debug" has Min/Max but its type bool does not satisfy cmp.Ordered`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := boundWorld(t, []string{"bin"}, nil, nil, tc.md, boundCfg{Version: 1, Port: 80})
			joined := ""
			for _, err := range w.c.All() {
				joined += err.Error() + "\n"
			}
			if !strings.Contains(joined, tc.want) {
				t.Errorf("want %q in:\n%s", tc.want, joined)
			}
		})
	}
}
