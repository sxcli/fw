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

import "sxcli.dev/conf/engine"

// CoreFeature identifies one suppressible piece of the framework core's
// configuration surface.
type CoreFeature int

const (
	// FeatureConfigFile is the --config,-c argument: the explicit
	// configuration file path. Suppressing it leaves the location
	// search as the only file source.
	FeatureConfigFile CoreFeature = iota
	// FeatureWriteConfig is the --write-config argument.
	FeatureWriteConfig
	// FeatureDisable is the --disable service control. The three
	// controls are OFF by default: reshaping the resolved service
	// set at invocation time is a deliberate capability, and a
	// binary that never opted in carries no such surface — the
	// author turns one on with Enable.
	FeatureDisable
	// FeatureEnable is the --enable service control; off by default.
	FeatureEnable
	// FeatureOverride is the --override service control; off by
	// default.
	FeatureOverride
	// FeatureHelp is the --help,-h argument (argument-only: help has
	// no environment door).
	FeatureHelp
	// FeatureValidateConfig is the --validate-config argument.
	FeatureValidateConfig
	// FeatureConfigMaxBytes is the --config-max-bytes argument, the
	// operator's run-scoped override of the config file size cap.
	FeatureConfigMaxBytes
	// FeatureApplets is the --applets listing, the binary's catalog
	// door for humans.
	FeatureApplets
	// FeatureUpgradeConfig is the --upgrade-config tool, its
	// --from-version companion included (inert alone).
	FeatureUpgradeConfig
	// FeatureSCMDebug is the windows-only --scm-debug argument: it runs
	// the service pipeline under svc/debug outside the service manager,
	// for testing. It is argument-only (never env or config file,
	// absent from --help) and off by default — a binary exposes it
	// with Enable. On other platforms the token is an unknown
	// argument.
	FeatureSCMDebug
)

// coreFeatureLongs maps features to the long argument names of the
// core's config fields.
var coreFeatureLongs = map[CoreFeature]string{
	FeatureConfigFile:     "config",
	FeatureWriteConfig:    "write-config",
	FeatureDisable:        "disable",
	FeatureEnable:         "enable",
	FeatureOverride:       "override",
	FeatureHelp:           "help",
	FeatureValidateConfig: "validate-config",
	FeatureUpgradeConfig:  "upgrade-config",
	FeatureConfigMaxBytes: "config-max-bytes",
	FeatureApplets:        "applets",
}

// suppressedCore holds the long names of explicitly suppressed core
// fields; effectiveSuppressedCore adds the default-off controls that
// were never enabled, and Main passes the result into the
// configuration machinery.
var suppressedCore []string

// defaultOffControls are the control features an author must Enable;
// enabledControls records the opt-ins.
var defaultOffControls = map[CoreFeature]bool{
	FeatureDisable:  true,
	FeatureEnable:   true,
	FeatureOverride: true,
}
var enabledControls = map[CoreFeature]bool{}

// effectiveSuppressedCore is the schema's view: explicit suppresses
// plus every control the author never enabled.
func effectiveSuppressedCore() []string {
	out := append([]string(nil), suppressedCore...)
	for feature := range defaultOffControls {
		if !enabledControls[feature] {
			out = append(out, coreFeatureLongs[feature])
		}
	}
	return out
}

// configMaxBytes is the effective config file size cap; Main passes
// it into the configuration machinery. Zero means unlimited, so the
// declaration carries the default instead of a sentinel.
var configMaxBytes uint64 = engine.DefaultConfigMaxBytes

// ConfigMaxBytes sets this binary's config file size cap in bytes; a
// file larger than the cap is refused with a loud startup error. The
// default (1 MiB) covers any sane configuration; zero removes the
// cap entirely, and the operator may override either choice for one
// run with --config-max-bytes (0 = unlimited there too). Like
// Suppress this is a build-time property of the binary: call it from
// main() or an init() before Main.
func ConfigMaxBytes(limit uint64) {
	configMaxBytes = limit
}

// scmDebugEnabled records the FeatureSCMDebug opt-in; the windows
// platform layer consults it.
var scmDebugEnabled bool

// Enable turns on default-off core features: the three service
// controls (FeatureDisable/FeatureEnable/FeatureOverride) and
// FeatureSCMDebug. Enabling a default-on feature is a violation
// (Suppress is the counterpart for those). Enable compiles and runs on
// every platform — on one where the feature cannot exist it is a
// harmless no-op — so a shared main() builds everywhere. Like Suppress
// it is a build-time property: call it before Main.
func Enable(features ...CoreFeature) {
	for _, feature := range features {
		if feature == FeatureSCMDebug {
			scmDebugEnabled = true
		} else if defaultOffControls[feature] {
			enabledControls[feature] = true
		} else {
			defaultCollector.Fail("Enable: feature %d is not a default-off feature", feature)
		}
	}
}

// Suppress removes core configuration features from this binary. A
// suppressed feature vanishes from the core's schema entirely: its
// argument becomes unknown (an error in the strict pass), its
// environment variable is never consulted, and its key appearing in a
// config file's core section is a loud startup error — operators learn
// it is not honored instead of wondering why it is ignored.
//
// Call Suppress from the consumer's main() or an init() function,
// before Main; it is a build-time property of the binary, not runtime
// configuration.
func Suppress(features ...CoreFeature) {
	for _, feature := range features {
		if feature == FeatureSCMDebug || defaultOffControls[feature] {
			defaultCollector.Fail("Suppress: feature %d is off by default; Enable is its switch", feature)
		} else if long, known := coreFeatureLongs[feature]; known {
			dup := false
			for _, existing := range suppressedCore {
				dup = dup || existing == long
			}
			if !dup {
				suppressedCore = append(suppressedCore, long)
				if feature == FeatureUpgradeConfig {
					suppressedCore = append(suppressedCore, "from-version")
				}
			}
		} else {
			defaultCollector.Fail("Suppress: unknown core feature %d", feature)
		}
	}
}
