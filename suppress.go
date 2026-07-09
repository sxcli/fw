package sxclifw

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
	// FeatureDisable is the --disable service control.
	FeatureDisable
	// FeatureEnable is the --enable service control.
	FeatureEnable
	// FeatureOverride is the --override service control.
	FeatureOverride
)

// coreFeatureLongs maps features to the long argument names of the
// core's config fields.
var coreFeatureLongs = map[CoreFeature]string{
	FeatureConfigFile:  "config",
	FeatureWriteConfig: "write-config",
	FeatureDisable:     "disable",
	FeatureEnable:      "enable",
	FeatureOverride:    "override",
}

// suppressedCore holds the long names of suppressed core fields; Main
// passes it into the configuration machinery.
var suppressedCore []string

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
		if long, known := coreFeatureLongs[feature]; known {
			suppressedCore = append(suppressedCore, long)
		} else {
			defaultCollector.Fail("Suppress: unknown core feature %d", feature)
		}
	}
}
