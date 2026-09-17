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

// The three control features live HERE, not in the root package: to
// name one you must import this package, and the import is what puts
// the controls in the binary — suppressing a control that is not
// there is a compile error, not a runtime verdict. The constants are
// untyped: the root package's CoreFeature type would cycle through
// fw's own tests, and untyped they convert at the Suppress call site
// anyway. Values from 64 up; the root package keeps its own below 64,
// so the two ranges can never collide.
const (
	// FeatureDisableService is the --disable service control.
	FeatureDisableService = 64
	// FeatureEnableService is the --enable service control.
	FeatureEnableService = 65
	// FeatureOverrideService is the --override service control.
	FeatureOverrideService = 66
)
