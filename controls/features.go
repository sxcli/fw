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

package controls

// The Suppress identities of the operator's three service controls —
// --disable, --enable and --override — live here, not in the root
// package.
//
//   - to pass one of these constants to fw.Suppress you must import
//     this package — and importing it is also what compiles the
//     control machinery into the binary at all (this package's init
//     registers it; unimported, the linker drops it — the spec's
//     Suppress passage). A binary without the import has no
//     --disable, --enable or --override, and source that tries to
//     suppress them does not compile: the mistake cannot survive to
//     runtime
//   - the constants are untyped: the root package's CoreFeature type
//     would cycle through fw's own tests, and untyped they convert
//     at the Suppress call site anyway
//   - values start at 64; the root package keeps its own below 64,
//     so the two ranges can never collide
const (
	// FeatureDisableService is the --disable service control.
	FeatureDisableService = 64
	// FeatureEnableService is the --enable service control.
	FeatureEnableService = 65
	// FeatureOverrideService is the --override service control.
	FeatureOverrideService = 66
)
