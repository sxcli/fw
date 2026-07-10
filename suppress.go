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
	// FeatureHelp is the --help,-h argument (argument-only: help has
	// no environment door).
	FeatureHelp
	// FeatureSCMDebug is the windows-only --scm-debug argument: it runs
	// the service pipeline under svc/debug outside the service manager,
	// for testing. It is argument-only (never env or config file,
	// absent from --help) and the only default-off feature — a binary
	// exposes it with Enable. On other platforms the token is an
	// unknown argument.
	FeatureSCMDebug
)

// coreFeatureLongs maps features to the long argument names of the
// core's config fields.
var coreFeatureLongs = map[CoreFeature]string{
	FeatureConfigFile:  "config",
	FeatureWriteConfig: "write-config",
	FeatureDisable:     "disable",
	FeatureEnable:      "enable",
	FeatureOverride:    "override",
	FeatureHelp:        "help",
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
// maxConfigSize is the config file size cap; Main passes it into the
// configuration machinery.
var maxConfigSize int64

// MaxConfigSize sets this binary's config file size cap in bytes; a
// file larger than the cap is refused with a loud startup error. The
// default (1 MiB) covers any sane configuration. Like Suppress this is
// a build-time property of the binary: call it from main() or an init()
// before Main.
func MaxConfigSize(limit int64) {
	if limit > 0 {
		maxConfigSize = limit
	} else {
		defaultCollector.Fail("MaxConfigSize: the limit must be positive, got %d", limit)
	}
}

// scmDebugEnabled records the FeatureSCMDebug opt-in; the windows
// platform layer consults it.
var scmDebugEnabled bool

// Enable turns on default-off core features; FeatureSCMDebug is
// currently the only one. Enabling a default-on feature is a violation
// (Suppress is the counterpart for those). Enable compiles and runs on
// every platform — on one where the feature cannot exist it is a
// harmless no-op — so a shared main() builds everywhere. Like Suppress
// it is a build-time property: call it before Main.
func Enable(features ...CoreFeature) {
	for _, feature := range features {
		if feature == FeatureSCMDebug {
			scmDebugEnabled = true
		} else {
			defaultCollector.Fail("Enable: feature %d is not a default-off feature", feature)
		}
	}
}

func Suppress(features ...CoreFeature) {
	for _, feature := range features {
		if feature == FeatureSCMDebug {
			defaultCollector.Fail("Suppress: FeatureSCMDebug is off by default; Enable is its switch")
		} else if long, known := coreFeatureLongs[feature]; known {
			dup := false
			for _, existing := range suppressedCore {
				dup = dup || existing == long
			}
			if !dup {
				suppressedCore = append(suppressedCore, long)
			}
		} else {
			defaultCollector.Fail("Suppress: unknown core feature %d", feature)
		}
	}
}
