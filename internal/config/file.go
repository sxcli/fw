package config

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/sxcli/sxcli-fw/internal/fail"
)

// LoadFiles discovers, transcodes and parses the configuration files of
// one invocation. explicit is the resolved --config path: when non-empty
// it replaces the location search entirely and must exist. Otherwise
// every base path in src.Locations is probed with ".json" plus every
// registered provider extension; more than one existing candidate at the
// same location is ambiguous and a startup violation, as is a file whose
// extension no provider handles.
func LoadFiles(c *fail.Collector, src Sources, explicit string) *Files {
	f := &Files{}
	byExt := map[string]Provider{}
	var exts []string
	for _, p := range src.Providers {
		for _, ext := range p.Extensions() {
			if _, taken := byExt[ext]; !taken {
				byExt[ext] = p
				exts = append(exts, ext)
			}
		}
	}
	if explicit != "" {
		ext := strings.TrimPrefix(filepath.Ext(explicit), ".")
		provider, known := byExt[ext]
		if ext == "json" || known {
			if r, err := src.Open(explicit); err == nil {
				f.parse(c, explicit, r, provider)
			} else {
				c.Fail("config file %q: %v", explicit, err)
			}
		} else {
			c.Fail("config file %q: no format provider handles extension %q", explicit, ext)
		}
	} else {
		for _, loc := range src.Locations {
			open := src.Open
			if loc.Pinned {
				open = src.OpenPinned
			}
			if open != nil {
				type candidate struct {
					path     string
					reader   io.ReadCloser
					provider Provider // nil for native json
				}
				var found []candidate
				for _, ext := range append([]string{"json"}, exts...) {
					path := loc.Base + "." + ext
					if r, err := open(path); err == nil {
						found = append(found, candidate{path: path, reader: r, provider: byExt[ext]})
					} else if !errors.Is(err, fs.ErrNotExist) {
						c.Fail("config file %q: %v", path, err)
					}
				}
				if len(found) == 1 {
					f.parse(c, found[0].path, found[0].reader, found[0].provider)
				} else if len(found) > 1 {
					var paths []string
					for _, cand := range found {
						paths = append(paths, cand.path)
						cand.reader.Close()
					}
					c.Fail("ambiguous configuration at %q: %v all exist", loc.Base, paths)
				}
			} else {
				c.Fail("config location %q: pinned location without a pinned opener", loc.Base)
			}
		}
	}
	return f
}

// parse transcodes one file to JSON when a provider is given, decodes
// its top-level service sections and records the used provider.
func (f *Files) parse(c *fail.Collector, path string, r io.ReadCloser, provider Provider) {
	var in io.Reader = r
	if provider != nil {
		if jr, err := provider.ToJSON(r); err == nil {
			in = jr
		} else {
			c.Fail("config file %q: %v", path, err)
			in = nil
		}
	}
	if in != nil {
		section := map[string]json.RawMessage{}
		dec := json.NewDecoder(in)
		dec.UseNumber()
		if err := dec.Decode(&section); err == nil {
			f.sections = append(f.sections, section)
			if provider != nil {
				used := false
				for _, u := range f.Used {
					used = used || u == provider
				}
				if !used {
					f.Used = append(f.Used, provider)
				}
			}
		} else {
			c.Fail("config file %q: %v", path, err)
		}
	}
	r.Close()
}

// applyFiles writes every loaded file's sections into the schema's
// config structs, file by file so that later files override earlier
// ones. Sections of services outside the schema are ignored — the same
// file serves every applet of the binary — but unknown keys inside an
// owned section are violations.
func (s *Schema) applyFiles(c *fail.Collector, files *Files) {
	for _, section := range files.sections {
		for _, svc := range s.services {
			if raw, present := section[svc.id]; present {
				applyObject(c, svc, svc.fields, 0, raw, svc.id)
			}
		}
	}
}

// applyObject applies one json object to the fields living at the given
// json-path depth; fields is the subset whose JSONPath matches the path
// walked so far.
func applyObject(c *fail.Collector, svc *serviceSchema, fields []*Field, depth int, raw json.RawMessage, where string) {
	object := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &object); err == nil {
		for key, value := range object {
			var leaf *Field
			var nested []*Field
			for _, f := range fields {
				if f.JSONPath[depth] == key {
					if len(f.JSONPath) == depth+1 {
						leaf = f
					} else {
						nested = append(nested, f)
					}
				}
			}
			if leaf != nil {
				if err := setFromJSON(svc.cfg.Elem().FieldByIndex(leaf.Path), value); err != nil {
					c.Fail("config %s.%s: %v", where, key, err)
				}
			} else if len(nested) > 0 {
				applyObject(c, svc, nested, depth+1, value, where+"."+key)
			} else {
				c.Fail("config %s: unknown key %q", where, key)
			}
		}
	} else {
		c.Fail("config %s: expected a json object: %v", where, err)
	}
}
