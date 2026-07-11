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

package config

import "sxcli.dev/fw/internal/fail"

// PeekCore is the first pipeline pass: it leniently extracts the core's
// own configuration from environment and arguments only — no file can be
// located before --config is known. File-sourced core values arrive
// later via Files.ApplyCore.
func PeekCore(c *fail.Collector, appletID string, src Sources) Core {
	var core Core
	before := c.Len()
	sch := NewSchema(c, appletID, &core, nil, src.SuppressCore)
	if c.Len() == before {
		sch.applyEnv(c, src.LookupEnv)
		sch.parseArgs(c, src.Args, true)
	}
	return core
}

// ApplyCore refills a fresh Core in full precedence order once the
// files are loaded: file sections, then environment, then arguments.
// This is the Core the closure resolution must use — a disable/enable/
// override list in a config file is only visible here.
func (f *Files) ApplyCore(c *fail.Collector, appletID string, src Sources) Core {
	var core Core
	before := c.Len()
	sch := NewSchema(c, appletID, &core, nil, src.SuppressCore)
	if c.Len() == before {
		sch.applyFiles(c, f)
		sch.applyEnv(c, src.LookupEnv)
		sch.parseArgs(c, src.Args, true)
	}
	return core
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
						target := svc.cfg.Elem().FieldByIndex(f.Path)
						var err error
						if f.IsSlice {
							err = setSliceFromEnv(target, value)
						} else {
							err = setFromString(target, value)
						}
						if err != nil {
							c.Fail("$%s: %v", f.EnvName, err)
						}
					}
				}
			}
		}
	}
}
