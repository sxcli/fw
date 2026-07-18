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

// Package fw is the sxcli framework — sxcli stands for Simple
// Extensible CLI — for building busybox-style single-binary tools. A
// consumer registers services (applets are just services that
// implement a specific interface) from package init() functions and calls
// Main(); the framework dispatches by argv[0] or by the first subcommand
// argument, resolves the dependency closure of the chosen applet, drives
// configuration from arguments, environment variables and config files,
// and runs the service lifecycle around the applet.
package fw

import "io"

// Stopper is the base lifecycle interface. Stop is called once, in exact
// reverse order of the successful Start calls, after the applet returns
// (or when startup is aborted after this service was started). Errors
// returned from Stop are logged; they never change the process exit code
// and never prevent the remaining Stop calls.
type Stopper interface {
	Stop() error
}

// Starter is implemented by services that need a running phase: anything
// startable must be stoppable. Start is called sequentially, in dependency
// order, after every closure member has been configured and just before
// the applet runs. A Start error aborts startup: already-started services
// are stopped in reverse order and the process exits non-zero.
//
// Dependencies are normally started before their dependents. Dependency
// cycles are legal (they are logged as warnings) but weaken that
// promise: a service participating in a cycle may receive Start while an
// injected fellow cycle member is not started yet, and must tolerate it.
type Starter interface {
	Stopper
	Start() error
}

// Configurable is implemented by services that own a Configuration struct
// (declared by its registration chain). The framework fills that struct
// in place with the merged configuration (defaults, config files,
// environment, arguments — least to most important) and then calls
// Configured as a notification; there is never a second config instance.
//
// Configured is called sequentially, in dependency order, after dependency
// fields have been injected — but before anything is started, so injected
// dependencies must not be treated as running yet. A Configured error
// aborts startup.
type Configurable interface {
	Configured() error
}

// Translator is the seam between Tr/TrN and an i18n catalog service.
// The core itself depends on it: a service declares
// Provides[Translator], and the core seeds it into every closure and
// runs its dependency subtree's Configured before anything renders —
// on the --help and --write-config short-circuits too, which
// otherwise run no lifecycle at all. Exactly one Translator may be
// registered; more than one is a startup violation. --disable still
// wins: the operator can force raw msgids.
//
// The contract mirrors the sink contract: the Translator must be
// operational when Configured returns. Start and Stop are not part of
// a Translator's job. If Configured fails, the core logs one buffered
// warning and proceeds untranslated — translations never fail a
// startup and never change an exit code.
type Translator interface {
	// Translate returns the msgid's translation for the active
	// locale. ok == false means untranslated: Tr renders the msgid
	// verbatim — msgids are the default text, gettext-style.
	Translate(msgid string) (translated string, ok bool)
	// TranslateN returns the translation of the plural pair for
	// quantity n; the catalog's plural formula picks the form. ok ==
	// false falls back to English rules over the msgids (n != 1
	// selects the plural).
	TranslateN(msgid, msgidPlural string, n int) (translated string, ok bool)
}

// ConfigurationUpdater is reserved: it will notify a service that its
// configuration struct has been re-filled with updated values. What
// triggers an update (file watch, signal, API) is deliberately undecided;
// no update is ever delivered in the current version.
type ConfigurationUpdater interface {
	ConfigurationUpdated() error
}

// ConfigFormatProvider is a service that transcodes a configuration
// format to and from the core's native JSON. The core matches config
// files to providers by file extension; Extensions returns the supported
// ones, lowercase and without the leading dot (e.g. "yaml", "yml"). An
// explicit --config file whose extension no registered provider handles
// is a startup error; the location search, by construction, only probes
// the extensions it knows and never sees other files.
//
// ToJSON and FromJSON must be pure stream transforms: the core uses them
// while discovering and loading config files — before anything is
// configured or started — so they must not depend on the provider's own
// configuration or lifecycle state. FromJSON serves configuration file
// generation (--write-config).
//
// Providers are ordinary services: registered cold, discovered by this
// interface, used statelessly. The provider whose extension matched an
// actually loaded file (or the --write-config target) is pulled into the
// closure and receives the normal lifecycle; unused providers stay cold
// and are ejected. A provider that wants an unconditional lifecycle
// declares a dependency or is forced in with --enable.
type ConfigFormatProvider interface {
	Extensions() []string
	ToJSON(in io.Reader) (io.Reader, error)
	FromJSON(in io.Reader) (io.Reader, error)
}

// Applet is a dispatchable entry point. The framework brackets Run with
// the application lifecycle: every closure member is configured and
// started before Run is invoked, and stopped in reverse order right after
// it returns. The process exits with Run's return value.
//
// An applet must not implement Starter or Stopper — registering one that
// does is a registration error. Panics inside Run are not recovered: the
// applet owns its own recovery and must return its error code from Run.
type Applet interface {
	Configurable
	Run() int
}
