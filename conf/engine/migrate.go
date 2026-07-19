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
	"fmt"
	"log/slog"
	"reflect"

	"sxcli.dev/fw/internal/fail"
)

// Step is one erased link of a section's migration chain: it converts
// the config document of one schema version to the next. Build steps
// with NewStep (or the front door's Step) — the generic constructor is
// what keeps the conversion typed.
type Step struct {
	from  uint32
	in    reflect.Type
	out   reflect.Type
	apply func(any) any
}

// NewStep declares the conversion from schema version `from` to the
// next: fn receives the strictly-parsed old document and returns its
// successor. The chain's link types are verified when the schema is
// built; the erased call can never mismatch at migration time.
func NewStep[From, To any](from uint32, fn func(From) To) Step {
	return Step{
		from:  from,
		in:    reflect.TypeOf((*From)(nil)).Elem(),
		out:   reflect.TypeOf((*To)(nil)).Elem(),
		apply: func(v any) any { return fn(v.(From)) },
	}
}

// chain is a section's validated version state: its current version,
// its Version field, and the migration steps (empty for a section that
// has never evolved — then file versions must match exactly).
type chain struct {
	steps   []Step
	version uint32 // the current schema version
	vfield  *Field
	fields  []*Field
}

// validateVersioning enforces the Version mandate on one section and
// validates its migration chain; a valid section is registered for
// versioned file application.
func (s *Schema) validateVersioning(c *fail.Collector, sec Section, fields []*Field) {
	var vf *Field
	for _, f := range fields {
		if len(f.JSONPath) == 1 && f.JSONPath[0] == "version" {
			vf = f
		}
	}
	if vf == nil || vf.Type.Kind() != reflect.Uint32 {
		c.Fail("section %q: the config struct must declare Version uint32 `json:\"version\"` — the file breadcrumb every future migration reads", sec.Name)
		return
	}
	// the version field permits ONLY the json annotation: no argument,
	// no env door (the env source is by definition current-dialect),
	// no dump exclusion (it would break the breadcrumb), no usage
	if vf.Long != "" || vf.Short != "" || vf.EnvName != "" || vf.NoEnv || vf.Transient || vf.Usage != "" {
		c.Fail("section %q: the version field allows only the json annotation — conf, env, dump and usage tags are errors", sec.Name)
		return
	}
	vf.NoEnv = true // the engine owns the exemption: no derived env name
	def := uint32(vf.root.Elem().FieldByIndex(vf.Path).Uint())
	ok := true
	if def == 0 {
		c.Fail("section %q: the factory default version is 0 — versions start at 1", sec.Name)
		ok = false
	}
	current := def
	if len(sec.Steps) > 0 {
		steps := sec.Steps
		for i := 1; i < len(steps); i++ {
			if steps[i].from != steps[i-1].from+1 {
				c.Fail("section %q: migration steps must be contiguous — the step from version %d follows the one from %d", sec.Name, steps[i].from, steps[i-1].from)
				ok = false
			}
			if steps[i].in != steps[i-1].out {
				c.Fail("section %q: the step from version %d takes %s but the previous step produces %s", sec.Name, steps[i].from, steps[i].in, steps[i-1].out)
				ok = false
			}
		}
		last := steps[len(steps)-1]
		if last.out != reflect.TypeOf(sec.Ptr).Elem() {
			c.Fail("section %q: the migration chain must terminate at the current config type %s, not %s", sec.Name, reflect.TypeOf(sec.Ptr).Elem(), last.out)
			ok = false
		}
		current = last.from + 1
		if def != current {
			c.Fail("section %q: the factory default version %d must equal the chain's terminal version %d", sec.Name, def, current)
			ok = false
		}
	}
	if ok {
		s.chains[sec.Name] = &chain{steps: sec.Steps, version: current, vfield: vf, fields: fields}
	}
}

// applyVersioned applies one file's section under the version rules:
// a versionless section is a partial in the current dialect (warned —
// partials stay versionless by design; stamping one turns absent keys
// into zeros at the next schema bump), a current-version section
// applies normally, an old one walks the migration chain, a
// newer-than-the-binary one is refused.
func (s *Schema) applyVersioned(c *fail.Collector, ch *chain, raw json.RawMessage, path, id string) {
	var peek struct {
		Version *uint32 `json:"version"`
	}
	if err := json.Unmarshal(raw, &peek); err != nil {
		c.Fail("config %s: expected a json object: %v", id, err)
		return
	}
	switch {
	case peek.Version == nil:
		slog.Warn("config section carries no version key; judged as the current dialect",
			"file", path, "section", id)
		applyObject(c, ch.fields, 0, raw, id)
	case *peek.Version == ch.version:
		applyObject(c, ch.fields, 0, raw, id)
	case *peek.Version > ch.version:
		c.Fail("config %s (%s): version %d was written by a newer schema than this binary's %d", id, path, *peek.Version, ch.version)
	case len(ch.steps) == 0:
		c.Fail("config %s (%s): version %d does not match the binary's %d and the section registers no migration", id, path, *peek.Version, ch.version)
	case *peek.Version < ch.steps[0].from:
		c.Fail("config %s (%s): version %d is no longer supported (oldest supported: %d)", id, path, *peek.Version, ch.steps[0].from)
	default:
		s.migrate(c, ch, raw, path, id, *peek.Version)
	}
}

// migrate strict-parses the old document against ITS OWN schema
// version, walks the chain to the current type, then copies the result
// into the live section whole — a versioned document is complete, and
// the migrated value IS the section's state at this file's precedence.
// Domain checks run on the migrated output.
func (s *Schema) migrate(c *fail.Collector, ch *chain, raw json.RawMessage, path, id string, from uint32) {
	idx := int(from - ch.steps[0].from)
	in := reflect.New(ch.steps[idx].in)
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(in.Interface()); err != nil {
		c.Fail("config %s (%s): version %d document: %v", id, path, from, err)
		return
	}
	v := in.Elem().Interface()
	for i := idx; i < len(ch.steps); i++ {
		v = ch.steps[i].apply(v)
	}
	migrated := reflect.New(reflect.TypeOf(v)).Elem()
	migrated.Set(reflect.ValueOf(v))
	// the engine owns the invariant: a conversion that forgot to set
	// the version cannot write 0
	migrated.FieldByIndex(ch.vfield.Path).SetUint(uint64(ch.version))
	where := fmt.Sprintf("config %s (%s, migrated from version %d)", id, path, from)
	for _, f := range ch.fields {
		if !f.Transient {
			target := f.root.Elem().FieldByIndex(f.Path)
			target.Set(migrated.FieldByIndex(f.Path))
			f.suspect = !checkDomain(c, where, f, target)
		}
	}
}
