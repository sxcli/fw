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
	"bytes"
	"encoding/json"
	"io/fs"
	"log/slog"
	"os"
	"reflect"

	"sxcli.dev/fw/internal/fail"
)

// UpgradeFile is the pure file transform behind --upgrade-config: it
// reads ONE file, migrates every schema-owned section to its current
// version, and writes the file back in its own format. No merge, no
// other sources, no defaults injection. Sections the schema does not
// own — a shared file serves other tools — pass through verbatim.
//
// A section carrying a version key migrates by it; a contradicting
// from entry is an error. A versionless section requires its version
// asserted via from ("section" → N) or the bare assertion (legal only
// when exactly one versionless owned section exists). The assertion
// declares the section a COMPLETE version-N document — the tool warns
// which top-level keys were absent from the input, since the chain
// materializes them.
func (s *Schema) UpgradeFile(c *fail.Collector, path string, from map[string]uint32, bare *uint32, src Sources) {
	files := LoadFiles(c, src, path)
	if c.Len() > 0 {
		return
	}
	if len(files.sections) != 1 {
		c.Fail("upgrade-config: %q did not load as one file", path)
		return
	}
	section := files.sections[0]
	var ids []string
	for name := range s.chains {
		ids = append(ids, name)
	}
	if _, rooted := s.chains[""]; rooted {
		// the root binding: fold the document's unclaimed keys into a
		// synthetic root entry and keep the claimed ones beside it
		if raw, present := rootObject(section, ids); present {
			filtered := map[string]json.RawMessage{"": raw}
			for name := range section {
				if _, owned := s.chains[name]; owned && name != "" {
					filtered[name] = section[name]
				}
			}
			section = filtered
		}
	}
	for name := range from {
		if _, owned := s.chains[name]; !owned {
			c.Fail("upgrade-config: --from-version names %q, which this binary's schema does not own", name)
		} else if _, present := section[name]; !present {
			c.Fail("upgrade-config: --from-version names %q, which the file does not contain", name)
		}
	}
	bareTarget, hasBareTarget := s.bareTarget(c, section, from, bare)
	if c.Len() > 0 {
		return
	}
	out := map[string]any{}
	for name, raw := range section {
		if ch, owned := s.chains[name]; owned {
			display := name
			if name == "" {
				display = s.appletID
			}
			isBare := hasBareTarget && name == bareTarget
			if upgraded := s.upgradeSection(c, ch, name, display, path, raw, from, isBare, bare); upgraded != nil {
				if name == "" {
					// the root binding: its keys ARE the document
					for k, v := range upgraded {
						out[k] = v
					}
				} else {
					out[name] = upgraded
				}
			}
		} else {
			// foreign sections pass through verbatim
			out[name] = json.RawMessage(raw)
		}
	}
	if c.Len() == 0 {
		s.writeUpgraded(c, path, out, src)
	}
}

// bareTarget resolves the bare --from-version assertion: legal only
// when exactly one owned, versionless, unscoped section is present.
// The second return distinguishes "no bare assertion" from the root
// binding, whose name is empty.
func (s *Schema) bareTarget(c *fail.Collector, section map[string]json.RawMessage, from map[string]uint32, bare *uint32) (string, bool) {
	var versionless []string
	for name, raw := range section {
		if _, owned := s.chains[name]; owned {
			var peek struct {
				Version *uint32 `json:"version"`
			}
			if json.Unmarshal(raw, &peek) == nil && peek.Version == nil {
				if _, scoped := from[name]; !scoped {
					versionless = append(versionless, name)
				}
			}
		}
	}
	if bare != nil {
		if len(versionless) == 1 {
			return versionless[0], true
		} else if len(versionless) == 0 {
			c.Fail("upgrade-config: a bare --from-version has no versionless section to apply to")
		} else {
			c.Fail("upgrade-config: a bare --from-version is ambiguous — versionless sections: %v; use section=N", versionless)
		}
	}
	return "", false
}

// upgradeSection transforms one owned section to its current version,
// returning the emitted object (nil on violation).
func (s *Schema) upgradeSection(c *fail.Collector, ch *chain, name, display, path string, raw json.RawMessage, from map[string]uint32, isBare bool, bare *uint32) map[string]any {
	var peek struct {
		Version *uint32 `json:"version"`
	}
	if err := json.Unmarshal(raw, &peek); err != nil {
		c.Fail("upgrade-config %s: expected a json object: %v", display, err)
		return nil
	}
	version := peek.Version
	if asserted, has := from[name]; has {
		if version != nil {
			c.Fail("upgrade-config %s: the file says version %d; a contradicting --from-version %s=%d is an error, not a tiebreak", display, *version, display, asserted)
			return nil
		}
		version = &asserted
		s.warnMaterialized(ch, display, asserted, raw)
	} else if version == nil && isBare {
		version = bare
		s.warnMaterialized(ch, display, *bare, raw)
	}
	if version == nil {
		c.Fail("upgrade-config %s: the section carries no version key — assert one with --from-version %s=N", display, display)
		return nil
	}
	switch {
	case *version == ch.version:
		return s.emitCurrent(c, ch, display, raw)
	case *version > ch.version:
		c.Fail("upgrade-config %s: version %d was written by a newer schema than this binary's %d", display, *version, ch.version)
	case len(ch.steps) == 0:
		c.Fail("upgrade-config %s: version %d does not match the binary's %d and the section registers no migration", display, *version, ch.version)
	case *version < ch.steps[0].from:
		c.Fail("upgrade-config %s: version %d is no longer supported (oldest supported: %d)", display, *version, ch.steps[0].from)
	default:
		idx := int(*version - ch.steps[0].from)
		in := reflect.New(ch.steps[idx].in)
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(in.Interface()); err != nil {
			c.Fail("upgrade-config %s (%s): version %d document: %v", display, path, *version, err)
			return nil
		}
		v := in.Elem().Interface()
		for i := idx; i < len(ch.steps); i++ {
			v = ch.steps[i].apply(v)
		}
		migrated := reflect.New(reflect.TypeOf(v)).Elem()
		migrated.Set(reflect.ValueOf(v))
		migrated.FieldByIndex(ch.vfield.Path).SetUint(uint64(ch.version))
		return emitStruct(ch, migrated)
	}
	return nil
}

// emitCurrent re-emits an already-current section: strict-parsed into
// the current type so unknown keys still fail, version stamped.
func (s *Schema) emitCurrent(c *fail.Collector, ch *chain, name string, raw json.RawMessage) map[string]any {
	instance := reflect.New(ch.vfield.root.Type().Elem())
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(instance.Interface()); err != nil {
		c.Fail("upgrade-config %s: current-version document: %v", name, err)
		return nil
	}
	instance.Elem().FieldByIndex(ch.vfield.Path).SetUint(uint64(ch.version))
	return emitStruct(ch, instance.Elem())
}

// emitStruct renders a section instance the way --write-config does:
// Transient skipped, empty values skipped (the documented caveat), the
// version key always present.
func emitStruct(ch *chain, instance reflect.Value) map[string]any {
	section := map[string]any{}
	buildSection(section, ch.fields, func(f *Field) reflect.Value {
		return instance.FieldByIndex(f.Path)
	})
	section["version"] = uint64(ch.version)
	return section
}

// warnMaterialized names the top-level keys of the asserted version's
// schema that the input lacks: the completeness assertion invents
// them, and the operator should review what it invented.
func (s *Schema) warnMaterialized(ch *chain, name string, asserted uint32, raw json.RawMessage) {
	var oldType reflect.Type
	if asserted == ch.version {
		oldType = ch.vfield.root.Type().Elem()
	} else if len(ch.steps) > 0 && asserted >= ch.steps[0].from && asserted < ch.version {
		oldType = ch.steps[asserted-ch.steps[0].from].in
	}
	if oldType == nil {
		return // the version checks will fail this section anyway
	}
	present := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &present) != nil {
		return
	}
	var absent []string
	for i := 0; i < oldType.NumField(); i++ {
		if tag, has := oldType.Field(i).Tag.Lookup("json"); has && tag != "version" {
			if _, there := present[tag]; !there {
				absent = append(absent, tag)
			}
		}
	}
	if len(absent) > 0 {
		slog.Warn("upgrade-config: the completeness assertion materializes keys absent from the input — review the result",
			"section", name, "asserted_version", asserted, "absent_keys", absent)
	}
}

// writeUpgraded writes the transformed document back in the file's own
// format, preserving its mode.
func (s *Schema) writeUpgraded(c *fail.Collector, path string, out map[string]any, src Sources) {
	js, err := json.MarshalIndent(out, "", "  ")
	if err == nil {
		var payload []byte
		if payload, err = transcode(js, path, src); err == nil {
			mode := fs.FileMode(0o600)
			if fi, serr := os.Stat(path); serr == nil {
				mode = fi.Mode().Perm()
			}
			err = os.WriteFile(path, payload, mode)
		}
	}
	if err != nil {
		c.Fail("upgrade-config: %v", err)
	}
}
