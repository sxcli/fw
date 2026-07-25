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

package fw

import (
	"sxcli.dev/conf/engine"
	"sxcli.dev/fw/internal/registry"
	"sxcli.dev/rules/grammar"
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

// CoreAlias is the operator-facing name of the framework core — its
// config section, the virtual root's name, and the synthesized
// introspection entry. No service may claim it.
const CoreAlias = engine.CoreID

// validServiceID reports whether id is package-shaped — the shared
// rule, one home: sxcli.dev/rules/grammar. The floor (at least one
// '/') makes the id and alias grammars disjoint by construction. The
// convention that an id BEGINS WITH the package's actual import path
// cannot be checked at runtime — that guarantee is sxcli-vet's.
func validServiceID(id string) bool { return grammar.ValidServiceID(id) }

// primaryAlias returns the name shown in listings and used for the
// env prefix and config section. Every catalog entry has one: the
// chain refuses to commit without a declared alias.
func primaryAlias(d *registry.Descriptor) string {
	return d.Aliases[0]
}

// validAlias reports whether a is a legal operator-facing name — the
// shared rule, one home: sxcli.dev/rules/grammar. One grammar for
// every alias origin: registration primary, secondary, Builder.Alias
// rename.
func validAlias(a string) bool { return grammar.ValidAlias(a) }
