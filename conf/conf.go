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
// engine: ONE annotated struct becomes a complete operator surface —
// config files (binary companion, system, user), environment variables
// and command-line arguments merged with strict validation, --help,
// --write-config and --upgrade-config served. The struct binds at the
// ROOT of the file: a config file is flat, {"version": 1, "listen":
// ":8080"}, the way the rest of the world writes them — sections are
// the framework's multi-service shape, not the front door's. The
// machinery lives in conf/engine; this package is its obvious
// sequencing.
//
//	cfg := Config{Listen: ":8080"}
//	l, served := conf.New("mytool", &cfg)
//	if served {
//		return // --help, --write-config or --upgrade-config answered, exit 0
//	}
//	leftovers, err := l.Result()
//
// Loading and the verdict are split so each result means one thing:
// served is a successfully consumed run (never an error), and
// Result's error is always a genuine failure — every violation of the
// whole pipeline, joined. The config structs hold their untouched
// defaults unless Result returns nil.
package conf

import (
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"

	"sxcli.dev/fw/conf/engine"
	"sxcli.dev/fw/internal/fail"
)

// Provider converts a config file format to and from the engine's
// native JSON; see the engine package.
type Provider = engine.Provider

// Step declares one link of a section's migration chain: the typed
// conversion from schema version `from` to the next. Old versions live
// on as plain json-only types; fn receives the strictly-parsed old
// document and returns its successor:
//
//	conf.Step(1, func(old ConfigV1) ConfigV2 { … })
func Step[From, To any](from uint32, fn func(From) To) engine.Step {
	return engine.NewStep(from, fn)
}

// Feature identifies one suppressible piece of the front door's
// surface: a core argument, a tier of the config location search, or
// a whole source. An argument feature's value IS the long argument
// name.
type Feature string

const (
	// FeatureConfig is the --config,-c argument: the explicit
	// configuration file path. Suppressing it leaves the location
	// search as the only file source.
	FeatureConfig Feature = "config"
	// FeatureWriteConfig is the --write-config argument.
	FeatureWriteConfig Feature = "write-config"
	// FeatureHelp is the --help,-h argument.
	FeatureHelp Feature = "help"

	// FeatureCompanionConfig is the pinned config next to the real
	// binary — a portable-app pattern; rarely wanted on unix.
	FeatureCompanionConfig Feature = "companion-config"
	// FeatureSystemConfig is the system-wide location (/etc on unix,
	// %ProgramData% on windows).
	FeatureSystemConfig Feature = "system-config"
	// FeatureUserConfig is the per-user location (the XDG config dir).
	FeatureUserConfig Feature = "user-config"

	// FeatureEnvironment is the environment as a whole: suppressing it
	// kills every derived name AND every explicit env-tag binding — a
	// section's documented ecosystem coupling (an HTTP_PROXY match,
	// say) dies with it. The author owns the surface. Unlike the
	// search tiers it applies to caller-supplied Sources too: the
	// environment is one identifiable door, not a location policy.
	FeatureEnvironment Feature = "environment"
)

// upgradeKnobs is the front door's own core contribution: the
// upgrade-config tool flags, run-scoped and argument-only like their
// engine siblings. They live here, not in the engine's Core, so a
// framework binary that does not serve them never parses them — a
// flag that parses but silently does nothing is worse than an unknown
// argument.
type upgradeKnobs struct {
	UpgradeConfig bool     `json:"upgradeConfig" conf:"upgrade-config" dump:"-" env:"-" usage:"migrate the --config file's sections to their current schema versions and exit"`
	FromVersion   []string `json:"fromVersion" conf:"from-version" dump:"-" env:"-" usage:"assert a versionless section's version, section=N (bare N when only one section qualifies)"`
}

// tierFeatures are the location-search features: suppressed tiers are
// simply never probed. Everything else routes to the core schema.
var tierFeatures = map[Feature]bool{
	FeatureCompanionConfig: true,
	FeatureSystemConfig:    true,
	FeatureUserConfig:      true,
}

// Loader is one loading run, from assembly through verdict: chain the
// knobs, terminal with Load, read the verdict with Result — one value
// through all three phases.
type Loader struct {
	name     string
	cfg      any
	steps    []engine.Step
	features []Feature
	maxSize  int64
	provs    []Provider
	src      *engine.Sources
	stdout   io.Writer
	stderr   io.Writer

	// the verdict, set by Load
	pos    []string
	err    error
	served bool
	loaded bool
}

// NewLoader starts a loading run for name — the identity behind the
// env prefix, the config search locations and the core section; it is
// deliberately not derived from argv[0], so renaming a binary never
// orphans its configuration. cfg is THE config struct, bound at the
// file's root — nobody needs more than one in a standalone tool.
func NewLoader(name string, cfg any) *Loader {
	return &Loader{name: name, cfg: cfg, stdout: os.Stdout, stderr: os.Stderr}
}

// New is the one-liner front door: exactly NewLoader(name, cfg).Load().
func New(name string, cfg any) (*Loader, bool) {
	return NewLoader(name, cfg).Load()
}

// Suppress removes pieces of the surface. A suppressed argument
// feature vanishes from the schema entirely: the argument becomes
// unknown, the env var is never read, and a config file mentioning it
// fails loudly. A suppressed location tier is never probed — tier
// suppression applies to the production search only; a caller-supplied
// Sources owns its own location policy.
func (l *Loader) Suppress(features ...Feature) *Loader {
	l.features = append(l.features, features...)
	return l
}

// Migrate attaches the config's migration chain, oldest step first —
// how a schema evolves without stranding the files already deployed.
// The chain is validated at Load: contiguous versions, each step's
// output feeding the next step's input, terminating at the current
// config type, whose factory default Version must equal the terminal
// version.
func (l *Loader) Migrate(steps ...engine.Step) *Loader {
	l.steps = append(l.steps, steps...)
	return l
}

// MaxSize caps config file sizes in bytes (default 1 MiB).
func (l *Loader) MaxSize(n int64) *Loader {
	l.maxSize = n
	return l
}

// Provider registers a config file format beyond the native JSON.
func (l *Loader) Provider(p Provider) *Loader {
	l.provs = append(l.provs, p)
	return l
}

// Sources replaces the production wiring (real argv, environment and
// filesystem) wholesale — the hermetic seam for tests and embedders.
func (l *Loader) Sources(src engine.Sources) *Loader {
	l.src = &src
	return l
}

// Output redirects the served surfaces: --help, --write-config and
// --upgrade-config output to stdout, best-effort warnings to stderr.
func (l *Loader) Output(stdout, stderr io.Writer) *Loader {
	l.stdout = stdout
	l.stderr = stderr
	return l
}

// errServed guards a Loader whose run was already served: Load's
// second return was true and there is no verdict to read.
var errServed = errors.New("conf: the run was already served (--help, --write-config or --upgrade-config); check Load's second return")

// errNotLoaded guards a Loader whose Load was never called.
var errNotLoaded = errors.New("conf: Result before Load — the run has not happened")

// Result delivers the run's verdict: the trailing positional
// arguments, or every violation of the pipeline joined. Nothing is
// validated here — validation already happened in Load; Result only
// reports it. On a non-nil error the config structs hold their
// untouched defaults.
func (l *Loader) Result() ([]string, error) {
	if l == nil || !l.loaded {
		return nil, errNotLoaded
	}
	if l.served {
		return nil, errServed
	}
	return l.pos, l.err
}

// Load is the terminal: discovery, files, environment, arguments,
// strict validation. A run asking for --help, --write-config or
// --upgrade-config is served here and Load returns (l, true) — help
// is best-effort (violations go to stderr, the schema still renders),
// write-config refuses to emit from a violated merge. Violations of a
// normal run are deferred to Result, whose error always means
// failure.
func (l *Loader) Load() (*Loader, bool) {
	c := &fail.Collector{}
	src := l.sources()
	l.loaded = true
	var peek engine.Core
	var peekUp upgradeKnobs
	engine.PeekCore(c, l.name, src, []engine.Contribution{engine.CoreContrib(&peek), {Ptr: &peekUp}})
	if c.Len() == 0 && peekUp.UpgradeConfig {
		return l.upgrade(c, src, peek, peekUp)
	}
	if c.Len() == 0 {
		files := engine.LoadFiles(c, src, l.explicitPath(src, peek))
		if c.Len() == 0 {
			var core engine.Core
			var up upgradeKnobs
			sch := engine.NewSchema(c, l.name, []engine.Contribution{engine.CoreContrib(&core), {Ptr: &up}}, l.rootSection(), src.SuppressCore)
			if c.Len() == 0 {
				saved := l.snapshot()
				loaded := sch.Apply(c, files, src)
				err := errors.Join(c.All()...)
				if peek.Help {
					// best-effort by decree: a broken config file must
					// never take --help down with it
					if err != nil {
						fmt.Fprintf(l.stderr, "error: %v\n", err)
					}
					sch.WriteHelp(l.stdout)
					l.restore(saved)
					l.served = true
				} else if peek.WriteConfig && err == nil {
					if werr := sch.WriteMerged(l.stdout, peek.Config, src); werr == nil {
						l.restore(saved)
						l.served = true
					} else {
						l.restore(saved)
						l.err = fmt.Errorf("write-config: %w", werr)
					}
				} else if err != nil {
					l.restore(saved)
					l.err = err
				} else {
					l.pos = loaded.Positionals
				}
				return l, l.served
			}
		}
	}
	l.err = errors.Join(c.All()...)
	return l, false
}

// upgrade serves --upgrade-config: the pure file transform. It never
// loads configuration — the schema is built only for its chains and
// fields, and the caller's structs stay untouched by construction.
func (l *Loader) upgrade(c *fail.Collector, src engine.Sources, peek engine.Core, knobs upgradeKnobs) (*Loader, bool) {
	if peek.Config == "" {
		c.Fail("upgrade-config requires an explicit --config target")
	}
	from := map[string]uint32{}
	var bare *uint32
	for _, entry := range knobs.FromVersion {
		section, value, scoped := strings.Cut(entry, "=")
		if !scoped {
			section, value = "", entry
		}
		n, err := strconv.ParseUint(value, 10, 32)
		if err != nil || n == 0 {
			c.Fail("from-version %q: versions are positive integers", entry)
		} else if section == "" {
			if bare != nil {
				c.Fail("from-version: only one bare assertion is possible")
			}
			v := uint32(n)
			bare = &v
		} else if _, dup := from[section]; dup {
			c.Fail("from-version: %q is asserted twice", section)
		} else {
			from[section] = uint32(n)
		}
	}
	var core engine.Core
	var up upgradeKnobs
	sch := engine.NewSchema(c, l.name, []engine.Contribution{engine.CoreContrib(&core), {Ptr: &up}}, l.rootSection(), src.SuppressCore)
	if c.Len() == 0 {
		sch.UpgradeFile(c, peek.Config, from, bare, src)
	}
	if c.Len() > 0 {
		l.err = errors.Join(c.All()...)
		return l, false
	}
	l.served = true
	return l, true
}

// rootSection binds the sole config struct at the file's root.
func (l *Loader) rootSection() []engine.Section {
	return []engine.Section{{Name: "", Ptr: l.cfg, Steps: l.steps}}
}

// sources assembles the run's Sources: the production wiring unless
// replaced, with the chain's knobs applied on top.
func (l *Loader) sources() engine.Sources {
	var src engine.Sources
	if l.src != nil {
		src = *l.src
	} else {
		src = engine.ProductionSources(l.name)
		src.Locations = l.locations()
	}
	for _, f := range l.features {
		if f == FeatureEnvironment {
			src.LookupEnv = nil
		} else if !tierFeatures[f] {
			src.SuppressCore = append(src.SuppressCore, string(f))
		}
	}
	src.Providers = append(src.Providers, l.provs...)
	if l.maxSize > 0 {
		src.MaxSize = l.maxSize
	}
	return src
}

// locations composes the production search from the unsuppressed
// tiers, in merge order.
func (l *Loader) locations() []engine.Location {
	drop := map[Feature]bool{}
	for _, f := range l.features {
		drop[f] = true
	}
	var out []engine.Location
	if !drop[FeatureCompanionConfig] {
		if loc, ok := engine.CompanionLocation(l.name); ok {
			out = append(out, loc)
		}
	}
	if !drop[FeatureSystemConfig] {
		out = append(out, engine.SystemLocation(l.name))
	}
	if !drop[FeatureUserConfig] {
		if loc, ok := engine.UserLocation(l.name); ok {
			out = append(out, loc)
		}
	}
	return out
}

// explicitPath mirrors the framework's rule: a --config target that
// --write-config is about to create is not a load source yet.
func (l *Loader) explicitPath(src engine.Sources, peek engine.Core) string {
	out := peek.Config
	if peek.WriteConfig && out != "" && src.Stat != nil {
		if _, err := src.Stat(out); err != nil {
			out = ""
		}
	}
	return out
}

// snapshot copies the config struct's current value — the defaults —
// so a served or failed run can hand it back untouched.
func (l *Loader) snapshot() reflect.Value {
	v := reflect.ValueOf(l.cfg).Elem()
	saved := reflect.New(v.Type()).Elem()
	saved.Set(v)
	return saved
}

func (l *Loader) restore(saved reflect.Value) {
	reflect.ValueOf(l.cfg).Elem().Set(saved)
}
