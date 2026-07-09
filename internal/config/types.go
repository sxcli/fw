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
	Open         func(string) (io.ReadCloser, error) // os.Open in production; missing files must report fs.ErrNotExist
	OpenPinned   func(string) (io.ReadCloser, error) // symlink-refusing opener (O_NOFOLLOW-style) for pinned locations
	Providers    []Provider                          // registered format providers, registration order
	SuppressCore []string                            // long names of core fields the binary suppressed (fw.Suppress)
}

// Core is the framework core's own configuration, living under the
// reserved service id "core". Config cannot meaningfully come from a
// config file: the files are already loaded by the time such a value
// would be seen.
type Core struct {
	Config      string   `json:"config" arg:"config,c" usage:"path of the configuration file, replaces the location search"`
	WriteConfig bool     `json:"writeConfig" arg:"write-config" usage:"write the merged configuration to the --config target (or stdout) and exit"`
	Help        bool     `json:"help" arg:"help,h" usage:"print the applet's argument schema and exit"`
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
	EnvName   string   // resolved environment variable name; "" = file-only
	Usage     string
	Type      reflect.Type
	IsSlice   bool
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
}

// Loaded is the outcome of a strict Schema.Apply.
type Loaded struct {
	Positionals []string
}
