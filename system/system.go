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

// ArgInfo describes one argument of the schema true to an applet's
// resolved service set. The type is the conf engine's — schema
// description is conf's domain, and the completion engine consumes
// it without any framework dependency; this alias is fw's name for
// it.
type ArgInfo = engine.ArgInfo

// PosInfo describes one positional slot of the target's schema, in
// slot order; the conf engine's type, aliased like ArgInfo.
type PosInfo = engine.PosInfo

// Introspector is a TARGET-SCOPED read-only view: what one applet's
// resolved graph looks like, for services that implement completions,
// documentation generators and similar meta features outside the
// core. The view is built from the binary's catalog alone — no config
// files, no location search, no environment — so its answers are
// input-deterministic: same binary, same target, same answer, always.
type Introspector interface {
	// Applets returns the primary aliases of the binary's public
	// applets, in registration order — a binary-level fact, the same
	// on every view. Hidden and System applets are omitted.
	Applets() []string
	// SingleApplet reports the applet that would run with no
	// selector word — its primary alias — from the core's own
	// dispatch rules. Binary-level, the same on every view.
	SingleApplet() (string, bool)
	// ConfigExtensions returns the file extensions the binary's
	// format providers claim, "json" included. Binary-level.
	ConfigExtensions() []string
	// Services returns the primary aliases of the TARGET's resolved
	// graph — the core leading, then the resolved service set's
	// members in order. The binary view (target "") has no resolved
	// service set: nil.
	Services() []string
	// Describe returns the long-form description of a member of the
	// target's resolved graph (alias or id); "" for anything outside
	// the resolved service set — introspection does not reach past
	// the graph.
	Describe(ref string) string
	// Arguments returns the argument schema true to the target's
	// resolved service set.
	// args are the words BEFORE the completion cursor; today they are
	// inert (the solve is catalog-only), reserved for the
	// explicit-control-vocabulary era when line-carried controls
	// participate. The binary view answers nil.
	Arguments(args []string) []ArgInfo
	// Positionals returns the target's positional slots in order,
	// indexed slots first, the rest collector (when declared) last.
	// The binary view has none: nil.
	Positionals() []PosInfo
}

// System is the framework's facade service: the core's facilities
// as methods, one stable member instead of a system service per
// facility. It is an INTERFACE — the framework registers a private
// implementation wired to the running composition, so there is no
// public seam to replace the framework's guts, and consumers mock
// it trivially. Inject it like any service; ask it for what you
// need.
type System interface {
	// Introspector returns the target-scoped introspection view for
	// the applet the dispatch NAME names (never an id). The empty
	// name is the binary view: applet listing, no resolved service
	// set. An
	// unknown name — or a name that is not an applet — returns nil:
	// a completion caller can do nothing with prose, so nil means
	// "offer nothing".
	Introspector(applet string) Introspector
}
