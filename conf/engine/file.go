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

package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"

	"sxcli.dev/fw/internal/fail"
)

// LoadFiles discovers, transcodes and parses the configuration files of
// one invocation. explicit is the resolved --config path: when non-empty
// it replaces the location search entirely and must exist. Otherwise
// every base path in src.Locations is probed with ".json" plus every
// registered provider extension; more than one existing candidate at the
// same location is ambiguous and a startup violation, as is a file whose
// extension no provider handles.
//
// Existence and size are probed via Stat before any file is opened: an
// oversized config is never opened, read or parsed.
func LoadFiles(c *fail.Collector, src Sources, explicit string) *Files {
	f := &Files{maxSize: src.MaxSize}
	if f.maxSize <= 0 {
		f.maxSize = DefaultMaxSize
	}
	if src.Stat != nil {
		byExt := map[string]Provider{}
		var exts []string
		for _, p := range src.Providers {
			for _, ext := range p.Extensions() {
				if ext == "json" {
					c.Fail("format provider claims the native extension %q", ext)
				} else if _, taken := byExt[ext]; !taken {
					byExt[ext] = p
					exts = append(exts, ext)
				} else {
					c.Fail("format providers conflict over extension %q", ext)
				}
			}
		}
		if explicit != "" {
			ext := strings.TrimPrefix(filepath.Ext(explicit), ".")
			provider, known := byExt[ext]
			if ext == "json" || known {
				if size, err := src.Stat(explicit); err == nil {
					f.admit(c, explicit, size, src.Open, provider)
				} else {
					c.Fail("config file %q: %v", explicit, err)
				}
			} else {
				c.Fail("config file %q: no format provider handles extension %q", explicit, ext)
			}
		} else {
			for _, loc := range src.Locations {
				type candidate struct {
					path     string
					size     int64
					provider Provider // nil for native json
				}
				var found []candidate
				for _, ext := range append([]string{"json"}, exts...) {
					path := loc.Base + "." + ext
					if size, err := src.Stat(path); err == nil {
						found = append(found, candidate{path: path, size: size, provider: byExt[ext]})
					} else if !errors.Is(err, fs.ErrNotExist) {
						c.Fail("config file %q: %v", path, err)
					} else if loc.Pinned && src.Lstat != nil && src.Lstat(path) == nil {
						// Stat (follows links) saw nothing, Lstat sees
						// something: a dangling symlink squats on the
						// companion location — someone put it there
						c.Fail("config file %q: a dangling symlink occupies the pinned companion location", path)
					}
				}
				if len(found) == 1 {
					open := src.Open
					if loc.Pinned {
						open = src.OpenPinned
					}
					if open != nil {
						f.admit(c, found[0].path, found[0].size, open, found[0].provider)
					} else {
						c.Fail("config location %q: no opener available (pinned: %v)", loc.Base, loc.Pinned)
					}
				} else if len(found) > 1 {
					var paths []string
					for _, cand := range found {
						paths = append(paths, cand.path)
					}
					c.Fail("ambiguous configuration at %q: %v all exist", loc.Base, paths)
				}
			}
		}
	} else {
		c.Fail("config: no stat function provided")
	}
	return f
}

// admit is the gate between discovery and reading: the stat-reported
// size is checked against the cap BEFORE the file is opened — an
// oversized config is never opened, read or parsed.
func (f *Files) admit(c *fail.Collector, path string, size int64, open func(string) (io.ReadCloser, error), provider Provider) {
	if size <= f.maxSize {
		if r, err := open(path); err == nil {
			f.parse(c, path, r, provider)
		} else {
			c.Fail("config file %q: %v", path, err)
		}
	} else {
		c.Fail("config file %q: %d bytes exceeds the %d byte limit", path, size, f.maxSize)
	}
}

// cappedReader errors — loudly, never truncating silently — once more
// than the allowed number of bytes has been read. It is defense in
// depth behind the stat-time size gate: a file can grow between stat
// and open, and stat sizes lie for special files.
type cappedReader struct {
	r    io.Reader
	left int64
	path string
}

func (cr *cappedReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	cr.left -= int64(n)
	if cr.left < 0 && (err == nil || errors.Is(err, io.EOF)) {
		err = fmt.Errorf("config file %q exceeds the size limit", cr.path)
	}
	return n, err
}

// parse transcodes one file to JSON when a provider is given, decodes
// its top-level service sections and records the used provider. The raw
// stream is capped: an oversized file is a loud violation.
func (f *Files) parse(c *fail.Collector, path string, r io.ReadCloser, provider Provider) {
	var in io.Reader = &cappedReader{r: r, left: f.maxSize, path: path}
	if provider != nil {
		if jr, err := provider.ToJSON(in); err == nil {
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
			var trailing json.RawMessage
			if terr := dec.Decode(&trailing); terr != io.EOF {
				c.Fail("config file %q: trailing data after the configuration object", path)
			} else {
				f.sections = append(f.sections, section)
				f.paths = append(f.paths, path)
				if provider != nil {
					used := false
					for _, u := range f.Used {
						used = used || u == provider
					}
					if !used {
						f.Used = append(f.Used, provider)
					}
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
	// the composite core is several schema entries sharing one section
	// name: a file section is applied against the UNION of its owners'
	// fields, so a key is only unknown when no contribution has it
	var ids []string
	byID := map[string][]*Field{}
	for _, svc := range s.services {
		if _, seen := byID[svc.id]; !seen {
			ids = append(ids, svc.id)
		}
		byID[svc.id] = append(byID[svc.id], svc.fields...)
	}
	for i, section := range files.sections {
		for _, id := range ids {
			raw, present := section[id]
			if id == "" {
				// the root binding owns the document itself, minus the
				// keys named sections claim
				raw, present = rootObject(section, ids)
			}
			if present {
				where := id
				if id == "" {
					where = s.appletID
				}
				if ch, versioned := s.chains[id]; versioned {
					s.applyVersioned(c, ch, raw, files.paths[i], where)
				} else {
					applyObject(c, byID[id], 0, raw, where)
				}
			}
		}
	}
}

// rootObject re-assembles the file document without the keys owned by
// named sections: what remains is the root section's object.
func rootObject(doc map[string]json.RawMessage, ids []string) (json.RawMessage, bool) {
	named := map[string]bool{}
	for _, id := range ids {
		if id != "" {
			named[id] = true
		}
	}
	out := map[string]json.RawMessage{}
	for key, raw := range doc {
		if !named[key] {
			out[key] = raw
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	js, err := json.Marshal(out)
	return js, err == nil
}

// applyObject applies one json object to the fields living at the given
// json-path depth; fields is the subset whose JSONPath matches the path
// walked so far.
func applyObject(c *fail.Collector, fields []*Field, depth int, raw json.RawMessage, where string) {
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
				if leaf.Transient {
					c.Fail("config %s.%s: run-scoped, settable only by argument or environment", where, key)
				} else {
					target := leaf.root.Elem().FieldByIndex(leaf.Path)
					if err := setFromJSON(target, value); err != nil {
						c.Fail("config %s.%s: %v", where, key, err)
					} else {
						checkDomain(c, "config "+where+"."+key, leaf, target)
					}
				}
			} else if len(nested) > 0 {
				applyObject(c, nested, depth+1, value, where+"."+key)
			} else {
				c.Fail("config %s: unknown key %q", where, key)
			}
		}
	} else {
		c.Fail("config %s: expected a json object: %v", where, err)
	}
}
