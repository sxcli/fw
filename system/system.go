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

// Package system is the declarations of the framework's own service:
// the facade every fw binary carries, cataloged by fw's init through
// the ordinary registration chain — the core's facilities are a
// MEMBER, not runtime magic. This package is pure vocabulary: it
// imports nothing of fw, so fw imports it and registers its type
// (init registers whatever it wants — another package's type
// included).
package system

import (
	"reflect"

	"sxcli.dev/conf/engine"
)

// ID is the system service's identity.
const ID = "sxcli.dev/fw/system"

// ValueHint is the advisory declaration of what a field's value
// denotes — the engine's vocabulary, one home.
type ValueHint = engine.ValueHint

const (
	// HintNone declares nothing; the zero value.
	HintNone = engine.HintNone
	// HintFile declares the value names a file, existing or to be.
	HintFile = engine.HintFile
	// HintDirectory declares the value names a directory.
	HintDirectory = engine.HintDirectory
	// HintServiceID is fw's vocabulary — the first custom hint above
	// the engine's universal set: the value names a service
	// registered in this binary's catalog.
	HintServiceID = engine.HintCustom
)

// ArgInfo describes one argument of an applet's closure-true schema.
type ArgInfo struct {
	Service string       // owning service ALIAS (the operator name), "core" included
	Long    string       // long argument name, without dashes
	Short   string       // single-character short form
	Env     string       // environment variable name
	Usage   string       // untranslated help text; render via Tr
	Type    reflect.Type // field type; element type for slices
	IsSlice bool         // repeatable argument, comma-separated env, json array
	Allowed []any        // closed value domain from registration Metadata; values are of Type
	Doc     string       // long-form description from registration Metadata
	Hint    ValueHint    // advisory value denotation from registration Metadata; never enforced
}

// Introspector is the read-only view of the binary's composition,
// for services that implement completions, documentation generators
// and similar meta features outside the core.
type Introspector interface {
	// Applets returns the primary aliases of the binary's public
	// applets, in registration order.
	Applets() []string
	// SingleApplet reports the applet that would run with no
	// selector word — its primary alias — from the core's own
	// dispatch rules.
	SingleApplet() (string, bool)
	// Services returns the primary alias of every registered
	// service, the core leading.
	Services() []string
	// ConfigExtensions returns the file extensions the binary's
	// format providers claim, "json" included.
	ConfigExtensions() []string
	// Describe returns a service's long-form description; alias or
	// id, both vocabularies are legal.
	Describe(serviceID string) string
	// Arguments returns the closure-true argument schema the applet
	// would have if invoked with args — the words BEFORE the cursor.
	Arguments(appletID string, args []string) ([]ArgInfo, error)
}

// System is the framework's facade service: the core's facilities as
// methods, one stable member instead of a system service per
// facility. Inject it like any service; ask it for what you need.
type System struct {
	intro Introspector
}

// Introspector returns the composition's introspection facility.
func (s *System) Introspector() Introspector {
	return s.intro
}

// Wire attaches the running framework's facilities to the cataloged
// shell. The framework calls this at startup — services never do;
// the registration is ordinary, the wiring is the framework's
// implementation detail.
func (s *System) Wire(intro Introspector) {
	s.intro = intro
}
