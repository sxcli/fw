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
	"os"

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
	// FeatureSCMDebug is the --scm-debug argument of Windows builds:
	// it runs the service pipeline under svc/debug outside the service
	// manager, for testing. Argument-only, like the other tool knobs.
	// On other platforms the knob does not exist — the token is an
	// unknown argument and suppressing the feature is a silent no-op.
	FeatureSCMDebug
	// FeatureCompanionConfig is the first config file tier: the pinned
	// file beside the real binary. Suppressed, that location is never
	// searched.
	FeatureCompanionConfig
	// FeatureSystemConfig is the system-wide tier (/etc on unix,
	// %ProgramData% on windows).
	FeatureSystemConfig
	// FeatureUserConfig is the per-user tier (the XDG config dir).
	FeatureUserConfig
	// FeatureEnvironment is the environment as a source: suppressed, no
	// variable is read — derived names and explicit env tags alike.
	// The three tiers and this one are sources, not arguments: they
	// have no long name and never appear in --help.
	FeatureEnvironment
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

// sourceFeatures are the features that are sources rather than
// arguments: the three config file tiers and the environment.
// Suppressing one removes the source from every run; Main composes
// the locations and the environment lookup from what remains.
var sourceFeatures = map[CoreFeature]bool{
	FeatureCompanionConfig: true,
	FeatureSystemConfig:    true,
	FeatureUserConfig:      true,
	FeatureEnvironment:     true,
}

// suppressedSources records the suppressed source features.
var suppressedSources = map[CoreFeature]bool{}

// configLocations is the production location search minus the
// suppressed tiers.
func configLocations(name string) []engine.Location {
	return engine.TierLocations(name,
		!suppressedSources[FeatureCompanionConfig],
		!suppressedSources[FeatureSystemConfig],
		!suppressedSources[FeatureUserConfig])
}

// environmentLookup is the production environment, or nil when the
// environment is suppressed — the engine reads nothing through a nil
// lookup.
func environmentLookup() func(string) (string, bool) {
	var lookup func(string) (string, bool)
	if !suppressedSources[FeatureEnvironment] {
		lookup = os.LookupEnv
	}
	return lookup
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

// featureLong resolves a feature's long argument name: the core's
// own table first, then the platform's knobs, then the controls'
// registered features (present only by the sxcli.dev/fw/controls
// import).
func featureLong(feature CoreFeature) (string, bool) {
	long, known := coreFeatureLongs[feature]
	if !known {
		long, known = platformFeatureLongs[feature]
	}
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
// it is not honored instead of wondering why it is ignored. Source
// features (the three config file tiers, the environment) remove a
// source instead of an argument. Suppressing FeatureConfigFile takes
// --upgrade-config and --write-config with it: without --config
// neither has a file to work on.
//
// Call Suppress from the consumer's main() or an init() function,
// before Main; it is a build-time property of the binary, not runtime
// configuration.
func Suppress(features ...CoreFeature) {
	for _, feature := range features {
		if long, known := featureLong(feature); known {
			dup := false
			for _, existing := range suppressedCore {
				dup = dup || existing == long
			}
			if !dup {
				suppressedCore = append(suppressedCore, long)
				if feature == FeatureUpgradeConfig {
					suppressedCore = append(suppressedCore, "from-version")
				}
				if feature == FeatureConfigFile {
					// no --config, no file to upgrade or write: the two
					// verbs leave with it (spec §6, Core feature
					// suppression)
					Suppress(FeatureUpgradeConfig, FeatureWriteConfig)
				}
			}
		} else if sourceFeatures[feature] {
			suppressedSources[feature] = true
		} else if feature == FeatureSCMDebug {
			// a Windows-only knob on another platform: nothing to
			// remove, the state asked for already holds
		} else {
			defaultCollector.Fail("Suppress: unknown core feature %d", feature)
		}
	}
}
