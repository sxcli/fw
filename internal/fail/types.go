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

// Package fail provides the shared startup-error collector. Every
// internal phase — registration, resolution, injection, configuration —
// records its violations into one Collector owned by the framework core,
// so startup can fail once and report every problem together.
package fail

// Collector accumulates startup violations in occurrence order. The
// zero value is ready to use. Callers gate phases by comparing Len
// snapshots taken before and after a phase.
type Collector struct {
	errs []error
}
