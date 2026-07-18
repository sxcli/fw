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

package sxclifw

import (
	"sxcli.dev/fw/internal/registry"
)

// CoreID is the framework core's identity — the machine-facing name
// in the two-name model (spec §4): IDs are import-path-shaped, unique
// through Go's module namespace, referenced by code. The core's
// operator-facing name is CoreAlias.
const CoreID = "sxcli.dev/fw"

// IntrospectionID is the core Introspector's identity; its
// operator-facing name is IntrospectionAlias.
const IntrospectionID = CoreID + "/introspection"

// IntrospectionAlias is the Introspector's operator name — what
// --enable takes and listings show. Reserved, like CoreAlias.
const IntrospectionAlias = "introspection"

// validServiceID reports whether id is path-shaped: slash-separated,
// non-empty segments of lowercase letters, digits, dots, hyphens and
// underscores, each starting with a letter or digit. The convention
// that an id BEGINS WITH the package's import path cannot be checked
// at runtime — that guarantee is sxclivet's.
func validServiceID(id string) bool {
	ok := id != ""
	start := 0
	for i := 0; i <= len(id) && ok; i++ {
		if i == len(id) || id[i] == '/' {
			ok = i > start && isIDSegment(id[start:i])
			start = i + 1
		}
	}
	return ok
}

// isIDSegment validates one path segment of a service id.
func isIDSegment(s string) bool {
	ok := (s[0] >= 'a' && s[0] <= 'z') || (s[0] >= '0' && s[0] <= '9')
	for i := 1; i < len(s) && ok; i++ {
		ch := s[i]
		ok = (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '.' || ch == '-' || ch == '_'
	}
	return ok
}

// aliasesOf returns a descriptor's operator-facing names. The
// coexistence rule for old-style entries with none declared: the id
// IS the operator name. TODO(composition item 7): drop the fallback
// with the old registration path.
func aliasesOf(d *registry.Descriptor) []string {
	out := d.Aliases
	if len(out) == 0 {
		out = []string{d.ID}
	}
	return out
}

// primaryAlias returns the name shown in listings and used for the
// env prefix and config section.
func primaryAlias(d *registry.Descriptor) string {
	return aliasesOf(d)[0]
}

// validAlias reports whether a is a legal operator-facing name:
// lowercase letters, digits and hyphens, starting with a letter.
// Hyphens are legal here — env-var derivation maps them to
// underscores, so "cherry-pick" is finally a command name.
func validAlias(a string) bool {
	ok := a != "" && a[0] >= 'a' && a[0] <= 'z'
	for i := 1; i < len(a) && ok; i++ {
		ch := a[i]
		ok = (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-'
	}
	return ok
}
