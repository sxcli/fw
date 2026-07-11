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

package registry

import (
	"fmt"
	"io"
	"reflect"
	"strings"
)

// Dump writes a human-readable description of the registry's contents
// and collected errors to w.
func (r *Registry) Dump(w io.Writer) {
	fmt.Fprintf(w, "registry: %d service(s), %d error(s)\n", len(r.ordered), r.c.Len())
	for i, d := range r.ordered {
		fmt.Fprintf(w, "[%d] %s → %s\n", i+1, d.ID, d.Concrete)
		if len(d.Provides) > 0 {
			names := make([]string, len(d.Provides))
			for j, it := range d.Provides {
				names[j] = it.String()
			}
			fmt.Fprintf(w, "    provides: %s\n", strings.Join(names, ", "))
		}
		if d.ConfigPtr != nil {
			fmt.Fprintf(w, "    config:   %s\n", reflect.TypeOf(d.ConfigPtr))
		}
		if len(d.Deps) > 0 {
			fmt.Fprintf(w, "    deps:\n")
			for _, dep := range d.Deps {
				fmt.Fprintf(w, "      %-8s %s\n", dep.Name, describeDep(dep))
			}
		}
	}
	if r.c.Len() > 0 {
		fmt.Fprintf(w, "errors:\n")
		for _, err := range r.c.All() {
			fmt.Fprintf(w, "  - %v\n", err)
		}
	}
}

// describeDep renders one dependency field: its type, how it matches
// (first / by id / all), and whether it is optional.
func describeDep(dep DepField) string {
	var b strings.Builder
	if dep.IsSlice {
		b.WriteString("[]")
		b.WriteString(dep.Type.String())
		if len(dep.IDs) > 0 {
			b.WriteString(" (ids: ")
			b.WriteString(strings.Join(dep.IDs, ", "))
			b.WriteString("; plus every other enabled match)")
		} else {
			b.WriteString(" (all matches)")
		}
	} else {
		b.WriteString(dep.Type.String())
		if len(dep.IDs) > 0 {
			b.WriteString(" (id: ")
			b.WriteString(dep.IDs[0])
			b.WriteString(")")
		} else {
			b.WriteString(" (first match)")
		}
	}
	if dep.Optional {
		b.WriteString(" (optional)")
	}
	return b.String()
}
