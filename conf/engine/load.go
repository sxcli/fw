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

import "sxcli.dev/fw/internal/fail"

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
	return Loaded{Positionals: s.parseArgs(c, src.Args, false)}
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
						} else {
							checkDomain(c, "$"+f.EnvName, f, target)
						}
					}
				}
			}
		}
	}
}
