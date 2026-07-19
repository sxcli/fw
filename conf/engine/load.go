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
	"reflect"

	"sxcli.dev/fw/internal/fail"
)

// PeekCore is the first pipeline pass: it leniently fills the
// composite core from environment and arguments only — no file can be
// located before --config is known. File-sourced core values arrive
// later via Files.ApplyCore. The caller owns the contribution
// structs and must hand over pristine ones.
func PeekCore(c *fail.Collector, appletID string, src Sources, core []Contribution) {
	before := c.Len()
	sch := NewSchema(c, appletID, core, nil, src.SuppressCore)
	if c.Len() == before {
		sch.applyEnv(c, src.LookupEnv)
		sch.parseArgs(c, src.Args, true)
	}
}

// ApplyCore fills the composite core in full precedence order once
// the files are loaded: file sections, then environment, then
// arguments. These are the values the closure resolution must use — a
// control list in a config file is only visible here. The caller owns
// the contribution structs and must hand over pristine ones (slice
// values already filled by a peek would double up).
func (f *Files) ApplyCore(c *fail.Collector, appletID string, src Sources, core []Contribution) {
	before := c.Len()
	sch := NewSchema(c, appletID, core, nil, src.SuppressCore)
	if c.Len() == before {
		sch.applyFiles(c, f)
		sch.applyEnv(c, src.LookupEnv)
		sch.parseArgs(c, src.Args, true)
	}
}

// Apply is the strict pipeline pass over the full schema: files, then
// environment, then arguments, unknown argument = violation. It fills
// every member's config struct in place and returns the trailing
// positionals.
func (s *Schema) Apply(c *fail.Collector, files *Files, src Sources) Loaded {
	s.applyFiles(c, files)
	s.applyEnv(c, src.LookupEnv)
	return Loaded{Positionals: s.assignPositionals(c, s.parseArgs(c, src.Args, false))}
}

// assignPositionals binds the trailing bare tokens to the active
// config's declarations. With nothing declared the raw tail passes
// through; any declaration makes the schema own the tail entirely —
// missing required positionals and unclaimed surplus are violations.
func (s *Schema) assignPositionals(c *fail.Collector, tokens []string) []string {
	if len(s.posFields) == 0 && s.posRest == nil {
		return tokens
	}
	for i, f := range s.posFields {
		name := f.JSONPath[len(f.JSONPath)-1]
		if i < len(tokens) {
			target := f.root.Elem().FieldByIndex(f.Path)
			if err := setFromString(target, tokens[i]); err != nil {
				c.Fail("positional <%s>: %v", name, err)
				f.suspect = true
			} else {
				f.suspect = !checkDomain(c, "positional <"+name+">", f, target)
			}
		} else if !f.posOpt {
			c.Fail("missing required positional <%s>", name)
		}
	}
	var rest []string
	if len(tokens) > len(s.posFields) {
		rest = tokens[len(s.posFields):]
	}
	if s.posRest != nil {
		if len(rest) > 0 {
			f := s.posRest
			name := f.JSONPath[len(f.JSONPath)-1]
			target := f.root.Elem().FieldByIndex(f.Path)
			target.Set(reflect.MakeSlice(target.Type(), 0, len(rest)))
			f.suspect = false
			for _, tok := range rest {
				if err := appendFromString(target, tok); err != nil {
					c.Fail("positional <%s>: %v", name, err)
					f.suspect = true
				} else if len(f.Allowed) > 0 && !domainHas(f, target.Index(target.Len()-1)) {
					c.Fail("positional <%s>: value %v is not among the allowed values %v", name, target.Index(target.Len()-1).Interface(), f.Allowed)
					f.suspect = true
				}
			}
		}
	} else if len(rest) > 0 {
		c.Fail("unexpected positional %q — declare it with a pos tag or collect the tail with pos:\"rest\"", rest[0])
	}
	return nil
}

// PositionalFields exposes the active config's declared positionals:
// the indexed fields in order, and the trailing collector (nil when
// absent) — what help renderers and tooling enumerate.
func (s *Schema) PositionalFields() ([]*Field, *Field) {
	return s.posFields, s.posRest
}

// applyEnv writes every present environment variable into its field.
// Slice values are comma-separated and replace the field whole.
func (s *Schema) applyEnv(c *fail.Collector, lookup func(string) (string, bool)) {
	if lookup != nil {
		for _, svc := range s.services {
			for _, f := range svc.fields {
				if f.EnvName != "" {
					if value, present := lookup(f.EnvName); present {
						target := f.root.Elem().FieldByIndex(f.Path)
						var err error
						if f.IsSlice {
							err = setSliceFromEnv(target, value)
						} else {
							err = setFromString(target, value)
						}
						if err != nil {
							c.Fail("$%s: %v", f.EnvName, err)
							f.suspect = true
						} else {
							f.suspect = !checkDomain(c, "$"+f.EnvName, f, target)
						}
					}
				}
			}
		}
	}
}
