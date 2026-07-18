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

// App is a sealed composition: the product of Builder.Build. It owns
// its private registry of descriptor copies — instances born fresh at
// Build, composed aliases applied, members ordered by rank (Order
// entries first in sequence, the rest sorted by id) — and shares
// nothing with the catalog or with any other App: the catalog's
// entries stay stateless, and two Builds are fully independent, which
// is the test-isolation guarantee.
//
// The composition is immutable from Build on — the graph-immutability
// philosophy one layer up. Main (the run entry) arrives with the
// pipeline re-plumb; until then an App is composed state.
type App struct {
	reg *registry.Registry
}
