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

package graph

import (
	"reflect"

	"sxcli.dev/conf/fail"
)

// Inject writes every resolved binding into its owner's inject field.
// Target types were verified during resolution, so Set cannot mismatch;
// the one runtime failure mode is an inject field promoted through a nil
// embedded pointer, which is recorded per field instead of panicking.
// Fields with no targets (unmatched optional dependencies) are left
// untouched, so an optional slice stays nil rather than empty.
func (res Result) Inject(c *fail.Collector) {
	res.InjectExcept(c, nil)
}

// InjectExcept injects every member whose id is not in skip — the
// members another pass (the translator-first Configured, spec §7)
// already injected: re-injecting them would silently rewire fields
// their Configured may have legitimately touched.
func (res Result) InjectExcept(c *fail.Collector, skip map[string]bool) {
	for _, m := range res.Ordered {
		if !skip[m.Desc.ID] {
			v := reflect.ValueOf(m.Desc.Instance).Elem()
			for _, b := range m.Bindings {
				if len(b.Targets) > 0 {
					if f, err := v.FieldByIndexErr(b.Dep.Index); err == nil {
						if b.Dep.IsSlice {
							s := reflect.MakeSlice(f.Type(), 0, len(b.Targets))
							for _, target := range b.Targets {
								s = reflect.Append(s, reflect.ValueOf(target.Instance))
							}
							f.Set(s)
						} else {
							f.Set(reflect.ValueOf(b.Targets[0].Instance))
						}
					} else {
						c.Fail("service %q field %s: %v", m.Desc.ID, b.Dep.Name, err)
					}
				}
			}
		}
	}
}
