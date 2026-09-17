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
	"sxcli.dev/fw/internal/ctlhook"
)

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

// The values above stay below 64. The three service controls are
// features too, but their constants live in sxcli.dev/fw/controls —
// naming one requires the import that puts the controls in the
// binary — and that package claims its values from 64 up, so the two
// ranges can never collide.

// coreFeatureLongs maps features to the long argument names of the
// core's config fields.
var coreFeatureLongs = map[CoreFeature]string{
	FeatureConfigFile:     "config",
	FeatureWriteConfig:    "write-config",
	FeatureHelp:           "help",
	FeatureValidateConfig: "validate-config",
	FeatureUpgradeConfig:  "upgrade-config",
	FeatureConfigMaxBytes: "config-max-bytes",
	FeatureApplets:        "applets",
}

// suppressedCore holds the long names of suppressed core fields; Main
// passes it into the configuration machinery.
var suppressedCore []string

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

// scmDebugEnabled and scmDebugSuppressed record the two verbs'
// FeatureSCMDebug calls; each alone is redundant-or-effective and
// silent, the pair together is a startup contradiction judged at
// Build. The windows platform layer consults the enable side.
var scmDebugEnabled bool
var scmDebugSuppressed bool

// Enable turns on default-off core features; FeatureSCMDebug is
// currently the only one — the service controls opt in by importing
// sxcli.dev/fw/controls instead, so the linker can drop their code
// entirely. Enabling a feature that is already on is a no-op — the
// state asked for holds, the same ruling as naming an id twice in
// Accept — and an unknown feature is a violation. Enable compiles
// and runs on every platform — on one where the feature cannot exist
// it is a harmless no-op — so a shared main() builds everywhere.
// Like Suppress it is a build-time property: call it before Main.
func Enable(features ...CoreFeature) {
	for _, feature := range features {
		if feature == FeatureSCMDebug {
			scmDebugEnabled = true
		} else if _, known := featureLong(feature); !known {
			defaultCollector.Fail("Enable: unknown core feature %d", feature)
		}
	}
}

// featureLong resolves a feature's long argument name: the core's
// own table first, then the controls' registered features (present
// only by the sxcli.dev/fw/controls import).
func featureLong(feature CoreFeature) (string, bool) {
	long, known := coreFeatureLongs[feature]
	if !known && ctlhook.Registered != nil {
		long, known = ctlhook.Registered.FeatureLongs[int(feature)]
	}
	return long, known
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
		if feature == FeatureSCMDebug {
			// already off: redundant-true and silent — enabling it
			// elsewhere in the same binary is the contradiction, and
			// Build judges that pair loudly
			scmDebugSuppressed = true
		} else if long, known := featureLong(feature); known {
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
