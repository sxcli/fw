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
)

// CoreID is the framework core's identity — the machine-facing name
// in the two-name model (spec §4): IDs are import-path-shaped, unique
// through Go's module namespace, referenced by code. The core's
// operator-facing name is CoreAlias.
const CoreID = "sxcli.dev/fw"

// SystemAlias is the system service's operator name. Reserved at
// the commit so no user registration claims it before Build's
// composed-alias check would notice.
const SystemAlias = "system"

// CoreAlias is the operator-facing name of the framework core — its
// config section, the virtual root's name, and the synthesized
// introspection entry. No service may claim it.
const CoreAlias = engine.CoreID

// primaryAlias returns the name shown in listings and used for the
// env prefix and config section. Every catalog entry has one: the
// chain refuses to commit without a declared alias.
func primaryAlias(d *registry.Descriptor) string {
	return d.Aliases[0]
}
