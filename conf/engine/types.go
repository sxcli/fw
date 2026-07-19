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

// Package engine implements the configuration machinery of the sxcli
// framework: schema extraction from tagged config structs, the lenient
// and strict argument parsers, environment lookup, config file discovery
// and transcoding, and source merging with in-place struct filling.
// Like the other internal packages it is framework-ignorant: config
// structs arrive as named Sections built by the caller, and format
// providers arrive through a structural interface that the root
// package's ConfigFormatProvider satisfies implicitly.
package engine

import (
	"encoding/json"
	"io"
	"reflect"
)

// CoreID is the reserved service id of the framework core: the name
// of its config section, of the virtual root the resolver expands
// from, and of the synthesized introspection entry. The root package
// derives its reserved-id constant from this one.
const CoreID = "core"

// Provider is the structural twin of the root package's
// ConfigFormatProvider, redeclared here to avoid an import cycle; root
// provider instances satisfy it without adaptation.
type Provider interface {
	Extensions() []string
	ToJSON(in io.Reader) (io.Reader, error)
	FromJSON(in io.Reader) (io.Reader, error)
}

// Location is one config file search location: a base path without
// extension. A pinned location is security-sensitive — the binary
// companion — and its candidates are opened through Sources.OpenPinned,
// which must refuse symlinks so the file really lives at Base's
// directory.
type Location struct {
	Base   string
	Pinned bool
}

// Sources carries every external input of configuration loading, all
// injectable for hermetic tests.
type Sources struct {
	Args         []string                            // argv without the binary name and applet selector
	LookupEnv    func(string) (string, bool)         // os.LookupEnv in production
	Locations    []Location                          // search locations in merge order
	Stat         func(string) (int64, error)         // file size probe; missing files must report fs.ErrNotExist
	Lstat        func(string) error                  // pinned-location cross-check: nil when something occupies the path ITSELF (e.g. a dangling symlink Stat cannot see)
	Open         func(string) (io.ReadCloser, error) // os.Open in production; missing files must report fs.ErrNotExist
	OpenPinned   func(string) (io.ReadCloser, error) // symlink-refusing opener (O_NOFOLLOW-style) for pinned locations
	Providers    []Provider                          // registered format providers, registration order
	SuppressCore []string                            // long names of core fields the binary suppressed (fw.Suppress)
	MaxSize      int64                               // config file size cap in bytes; <=0 means the 1 MiB default
}

// DefaultMaxSize is the config file size cap applied when Sources does
// not set one: 1 MiB covers any sane configuration.
const DefaultMaxSize = 1 << 20

// Core is the engine's own configuration — the machinery knobs only,
// living under the reserved section name "core". All three are
// run-scoped (dump:"-"): excluded from --write-config output AND
// refused loudly from config files — a file setting them would be
// self-triggering (every run becoming help output, or a config write
// to an attacker-chosen path). writeConfig and help additionally
// carry env:"-": an inherited APPLETID_HELP=true would be the same
// persistent denial — they are argument-only. config keeps its env
// door (a legitimate deployment pattern; the pointed-at file still
// passes every gate). Anything else under "core" — the framework's
// service controls, say — arrives as a further Contribution.
type Core struct {
	Config      string `json:"config" conf:"config,c" dump:"-" usage:"path of the configuration file, replaces the location search"`
	WriteConfig bool   `json:"writeConfig" conf:"write-config" dump:"-" env:"-" usage:"write the merged configuration to the --config target (or stdout) and exit"`
	Help        bool   `json:"help" conf:"help,h" dump:"-" env:"-" usage:"print the applet's argument schema and exit"`
}

// Contribution is one flat struct claiming keys under the composite
// core section. The section name is fixed — that absence of a name is
// what distinguishes it from a Section. Contributors share the "core"
// namespace in every source; a json key claimed twice is a violation.
type Contribution struct {
	Ptr  any   // pointer to the struct; nil contributes nothing
	Meta *Meta // its own field metadata, nil when none declared
}

// Section is one named contributor to a schema: a config struct under
// its operator-facing section name. The framework maps its accepted
// services to sections; a standalone caller builds them directly. The
// engine never sees anything richer — this is the whole seam.
type Section struct {
	Name  string // section name: config-file key, env prefix
	Ptr   any    // pointer to the config struct; nil contributes nothing
	Meta  *Meta  // field metadata, nil when none declared
	Steps []Step // migration chain, oldest first; empty = never evolved
}

// ProbedField describes one settable config field for registration-time
// metadata validation: its type (element type for slices), slice-ness
// and current (default) value.
type ProbedField struct {
	Type    reflect.Type
	IsSlice bool
	Value   reflect.Value
}

// Meta is the internal, normalized form of a service's registration
// metadata (the root package's Metadata, validated and converted by
// its metadata check).
type Meta struct {
	Description string
	Fields      map[string]FieldMeta // keyed by go field name, "A.B" for nested
}

// ValueHint is the advisory declaration of what a field's value
// denotes. Unlike Allowed it is never enforced — a hinted file may not
// exist yet (--config with --write-config creates it); it travels the
// schema so tooling (completion, documentation) can act on it. The
// root package re-exports the constants under the same names.
type ValueHint int

const (
	HintNone ValueHint = iota
	HintFile
	HintDirectory
	HintServiceID
)

// FieldMeta annotates one config field. Allowed values are already
// converted to the field's own type.
type FieldMeta struct {
	Allowed []any
	Doc     string
	Hint    ValueHint
}

// Field is one settable config struct field.
type Field struct {
	ServiceID string
	Name      string   // go field name path for error messages, e.g. "Rotation.MaxAge"
	Path      []int    // reflect index path into the config struct
	JSONPath  []string // json object path inside the service's section
	Long      string   // long argument name; "" = file-only
	Short     string   // single-character short form; "" = none
	EnvName   string   // resolved environment variable name; "" = not env-settable
	NoEnv     bool     // env:"-": no environment variable, not even derived
	Usage     string
	Type      reflect.Type
	IsSlice   bool
	Transient bool  // dump:"-": run-scoped — excluded from --write-config output AND refused from config files
	Allowed   []any // closed value domain from registration metadata; values are of the field's type (element type for slices)
	Doc       string
	Hint      ValueHint // advisory value denotation from registration metadata

	root reflect.Value // the config struct this field lives in (its *Struct value)
}

// serviceSchema is the schema of one config struct under its section
// name; the composite core appears as several entries sharing "core".
type serviceSchema struct {
	id     string
	fields []*Field
}

// Schema is the full argument/env/file schema of one invocation: the
// core plus every closure member owning a config struct.
type Schema struct {
	chains   map[string]*chain // per-section version state (never the core)
	appletID string
	services []*serviceSchema
	long     map[string]*Field
	short    map[string]*Field
	owner    map[*Field]*serviceSchema
}

// Files is the parsed content of every loaded config file: one
// service-id → raw section map per file, in merge order (later files
// override earlier ones), plus the providers that transcoded them.
type Files struct {
	sections []map[string]json.RawMessage
	paths    []string // origin of each sections entry, for messages
	Used     []Provider
	maxSize  int64
}

// Loaded is the outcome of a strict Schema.Apply.
type Loaded struct {
	Positionals []string
}

// HelpSection is one service's schema for help rendering.
type HelpSection struct {
	ID     string
	Fields []*Field
}
