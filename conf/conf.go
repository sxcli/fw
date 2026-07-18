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

// Package conf is the loading front door of the sxcli configuration
// engine: one annotated struct becomes a complete operator surface —
// config files (binary companion, system, user), environment variables
// and command-line arguments merged with strict validation, --help and
// --write-config served. The machinery lives in conf/engine; this
// package is its obvious sequencing.
//
//	cfg := Config{Listen: ":8080"}
//	ldr, served := conf.New("mytool", &cfg)
//	if served {
//		return // --help or --write-config answered, exit 0
//	}
//	pos, err := ldr.Load()
//
// Construction and loading are split so each result means one thing:
// served is a successfully consumed run (never an error), and Load's
// error is always a genuine failure — every violation of the whole
// pipeline, joined. The config structs hold their untouched defaults
// unless Load returns nil.
package conf

import (
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"

	"sxcli.dev/fw/conf/engine"
	"sxcli.dev/fw/internal/fail"
)

// Provider converts a config file format to and from the engine's
// native JSON; see the engine package.
type Provider = engine.Provider

// LoaderBuilder assembles a loading run. Every knob is a chain
// method; Build is the terminal.
type LoaderBuilder struct {
	name     string
	sections []engine.Section
	suppress []string
	maxSize  int64
	provs    []Provider
	src      *engine.Sources
	stdout   io.Writer
	stderr   io.Writer
}

// Builder starts a loading run for name — the identity behind the env
// prefix, the config search locations and the core section; it is
// deliberately not derived from argv[0], so renaming a binary never
// orphans its configuration.
func Builder(name string) *LoaderBuilder {
	return &LoaderBuilder{name: name, stdout: os.Stdout, stderr: os.Stderr}
}

// Section adds one config struct under its section name: the file key,
// and the env-prefix namespace of its fields.
func (b *LoaderBuilder) Section(name string, cfg any) *LoaderBuilder {
	b.sections = append(b.sections, engine.Section{Name: name, Ptr: cfg})
	return b
}

// Suppress removes core arguments (long names, e.g. "write-config")
// from the schema entirely: the argument becomes unknown, the env var
// is never read, and a config file mentioning them fails loudly.
func (b *LoaderBuilder) Suppress(names ...string) *LoaderBuilder {
	b.suppress = append(b.suppress, names...)
	return b
}

// MaxSize caps config file sizes in bytes (default 1 MiB).
func (b *LoaderBuilder) MaxSize(n int64) *LoaderBuilder {
	b.maxSize = n
	return b
}

// Provider registers a config file format beyond the native JSON.
func (b *LoaderBuilder) Provider(p Provider) *LoaderBuilder {
	b.provs = append(b.provs, p)
	return b
}

// Sources replaces the production wiring (real argv, environment and
// filesystem) wholesale — the hermetic seam for tests and embedders.
func (b *LoaderBuilder) Sources(src engine.Sources) *LoaderBuilder {
	b.src = &src
	return b
}

// Output redirects the served surfaces: --help and --write-config
// output to stdout, best-effort warnings to stderr.
func (b *LoaderBuilder) Output(stdout, stderr io.Writer) *LoaderBuilder {
	b.stdout = stdout
	b.stderr = stderr
	return b
}

// Loader is a built loading run awaiting its verdict.
type Loader struct {
	pos []string
	err error
}

// errServed guards against a Build whose run was already served: the
// second return of Build was true and there is nothing to load.
var errServed = errors.New("conf: the run was already served (--help or --write-config); check Build's second return")

// Load delivers the run's verdict: the trailing positional arguments,
// or every violation of the pipeline joined. On a non-nil error the
// config structs hold their untouched defaults.
func (l *Loader) Load() ([]string, error) {
	if l == nil {
		return nil, errServed
	}
	return l.pos, l.err
}

// New is the single-struct front door: cfg becomes the section named
// after the binary itself. Exactly Builder(name).Section(name,
// cfg).Build().
func New(name string, cfg any) (*Loader, bool) {
	return Builder(name).Section(name, cfg).Build()
}

// Build runs the pipeline: discovery, files, environment, arguments,
// strict validation. A run asking for --help or --write-config is
// served here and Build returns (nil, true) — help is best-effort
// (violations go to stderr, the schema still renders), write-config
// refuses to emit from a violated merge. Violations of a normal run
// are deferred to Load, where an error always means failure.
func (b *LoaderBuilder) Build() (*Loader, bool) {
	c := &fail.Collector{}
	src := b.sources()
	var loader *Loader
	served := false
	var peek engine.Core
	engine.PeekCore(c, b.name, src, []engine.Contribution{engine.CoreContrib(&peek)})
	if c.Len() == 0 {
		files := engine.LoadFiles(c, src, b.explicitPath(src, peek))
		if c.Len() == 0 {
			var core engine.Core
			sch := engine.NewSchema(c, b.name, []engine.Contribution{engine.CoreContrib(&core)}, b.sections, src.SuppressCore)
			if c.Len() == 0 {
				saved := b.snapshot()
				loaded := sch.Apply(c, files, src)
				err := errors.Join(c.All()...)
				if peek.Help {
					// best-effort by decree: a broken config file must
					// never take --help down with it
					if err != nil {
						fmt.Fprintf(b.stderr, "error: %v\n", err)
					}
					sch.WriteHelp(b.stdout)
					b.restore(saved)
					served = true
				} else if peek.WriteConfig && err == nil {
					if werr := sch.WriteMerged(b.stdout, peek.Config, src); werr == nil {
						b.restore(saved)
						served = true
					} else {
						b.restore(saved)
						loader = &Loader{err: fmt.Errorf("write-config: %w", werr)}
					}
				} else if err != nil {
					b.restore(saved)
					loader = &Loader{err: err}
				} else {
					loader = &Loader{pos: loaded.Positionals}
				}
			}
		}
	}
	if loader == nil && !served {
		loader = &Loader{err: errors.Join(c.All()...)}
	}
	return loader, served
}

// sources assembles the run's Sources: the production wiring unless
// replaced, with the builder's knobs applied on top.
func (b *LoaderBuilder) sources() engine.Sources {
	src := engine.ProductionSources(b.name)
	if b.src != nil {
		src = *b.src
	}
	src.SuppressCore = append(src.SuppressCore, b.suppress...)
	src.Providers = append(src.Providers, b.provs...)
	if b.maxSize > 0 {
		src.MaxSize = b.maxSize
	}
	return src
}

// explicitPath mirrors the framework's rule: a --config target that
// --write-config is about to create is not a load source yet.
func (b *LoaderBuilder) explicitPath(src engine.Sources, peek engine.Core) string {
	out := peek.Config
	if peek.WriteConfig && out != "" && src.Stat != nil {
		if _, err := src.Stat(out); err != nil {
			out = ""
		}
	}
	return out
}

// snapshot copies every section struct's current value — the defaults —
// so a served or failed run can hand them back untouched.
func (b *LoaderBuilder) snapshot() []reflect.Value {
	out := make([]reflect.Value, len(b.sections))
	for i, sec := range b.sections {
		v := reflect.ValueOf(sec.Ptr).Elem()
		saved := reflect.New(v.Type()).Elem()
		saved.Set(v)
		out[i] = saved
	}
	return out
}

func (b *LoaderBuilder) restore(saved []reflect.Value) {
	for i, sec := range b.sections {
		reflect.ValueOf(sec.Ptr).Elem().Set(saved[i])
	}
}
