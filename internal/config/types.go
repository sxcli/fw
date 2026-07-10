// Package config implements the configuration machinery of the sxcli
// framework: schema extraction from tagged config structs, the lenient
// and strict argument parsers, environment lookup, config file discovery
// and transcoding, and source merging with in-place struct filling.
// Like the other internal packages it is framework-ignorant: descriptors
// arrive from the registry and format providers arrive through a
// structural interface that the root package's ConfigFormatProvider
// satisfies implicitly.
package config

import (
	"encoding/json"
	"io"
	"reflect"
)

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
	Open         func(string) (io.ReadCloser, error) // os.Open in production; missing files must report fs.ErrNotExist
	OpenPinned   func(string) (io.ReadCloser, error) // symlink-refusing opener (O_NOFOLLOW-style) for pinned locations
	Providers    []Provider                          // registered format providers, registration order
	SuppressCore []string                            // long names of core fields the binary suppressed (fw.Suppress)
	MaxSize      int64                               // config file size cap in bytes; <=0 means the 1 MiB default
}

// DefaultMaxSize is the config file size cap applied when Sources does
// not set one: 1 MiB covers any sane configuration.
const DefaultMaxSize = 1 << 20

// Core is the framework core's own configuration, living under the
// reserved service id "core". The run-scoped fields (config,
// writeConfig, help) carry dump:"-": excluded from --write-config
// output AND refused loudly from config files — a file setting them
// would be self-triggering (every run becoming help output, or a
// config write to an attacker-chosen path). writeConfig and help
// additionally carry env:"-": an inherited APPLETID_HELP=true would be
// the same persistent denial — they are argument-only. config keeps
// its env door (a legitimate deployment pattern; the pointed-at file
// still passes every gate).
type Core struct {
	Config      string   `json:"config" arg:"config,c" dump:"-" usage:"path of the configuration file, replaces the location search"`
	WriteConfig bool     `json:"writeConfig" arg:"write-config" dump:"-" env:"-" usage:"write the merged configuration to the --config target (or stdout) and exit"`
	Help        bool     `json:"help" arg:"help,h" dump:"-" env:"-" usage:"print the applet's argument schema and exit"`
	Disable     []string `json:"disable" arg:"disable" usage:"service ids to remove from the closure"`
	Enable      []string `json:"enable" arg:"enable" usage:"service ids to force into the closure"`
	Override    []string `json:"override" arg:"override" usage:"dependency remapping in from=to form"`
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
	Transient bool // dump:"-": run-scoped — excluded from --write-config output AND refused from config files
}

// serviceSchema is the schema of one service's config struct.
type serviceSchema struct {
	id     string
	cfg    reflect.Value // the *Struct registered via WithConfig
	fields []*Field
}

// Schema is the full argument/env/file schema of one invocation: the
// core plus every closure member owning a config struct.
type Schema struct {
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
