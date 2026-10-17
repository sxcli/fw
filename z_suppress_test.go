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
	"reflect"
	goos "runtime"
	"strings"
	"testing"

	"sxcli.dev/conf/engine"
	"sxcli.dev/fw/controls"
)

func TestSuppressMapsFeaturesToLongNames(t *testing.T) {
	old := suppressedCore
	t.Cleanup(func() { suppressedCore = old })
	suppressedCore = nil
	Suppress(FeatureWriteConfig, FeatureValidateConfig, FeatureHelp)
	want := []string{"write-config", "validate-config", "help"}
	if !reflect.DeepEqual(suppressedCore, want) {
		t.Errorf("got %v, want %v", suppressedCore, want)
	}
}

func TestSuppressDeduplicates(t *testing.T) {
	old := suppressedCore
	t.Cleanup(func() { suppressedCore = old })
	suppressedCore = nil
	Suppress(FeatureHelp)
	Suppress(FeatureHelp, FeatureHelp)
	if !reflect.DeepEqual(suppressedCore, []string{"help"}) {
		t.Errorf("repeated suppression must not duplicate: %v", suppressedCore)
	}
}

func TestConfigMaxBytes(t *testing.T) {
	old := configMaxBytes
	t.Cleanup(func() { configMaxBytes = old })
	if configMaxBytes != engine.DefaultConfigMaxBytes {
		t.Errorf("the declaration carries the default: %d", configMaxBytes)
	}
	ConfigMaxBytes(4096)
	if configMaxBytes != 4096 {
		t.Errorf("limit not set: %d", configMaxBytes)
	}
	// zero is the author choosing unlimited — a value, not a reset
	ConfigMaxBytes(0)
	if configMaxBytes != 0 {
		t.Error("zero must be stored as an explicit unlimited")
	}
}

func TestControlSuppressTrimsWithTheImport(t *testing.T) {
	// this test binary imports sxcli.dev/fw/controls, so trimming an
	// individual control is legal — the plain suppression road
	old := suppressedCore
	t.Cleanup(func() { suppressedCore = old })
	suppressedCore = nil
	Suppress(controls.FeatureOverrideService)
	if !reflect.DeepEqual(suppressedCore, []string{"override"}) {
		t.Errorf("an imported control must be suppressible: %v", suppressedCore)
	}
}

// --scm-debug is a knob of Windows builds: suppressing it there
// removes the argument like any other feature; elsewhere there is
// nothing to remove and the call is silent.
func TestSuppressSCMDebugFollowsThePlatform(t *testing.T) {
	old := suppressedCore
	t.Cleanup(func() { suppressedCore = old })
	suppressedCore = nil
	before := defaultCollector.Len()
	Suppress(FeatureSCMDebug)
	if defaultCollector.Len() != before {
		t.Errorf("suppressing the feature is never a violation: %v", defaultCollector.All())
	}
	if contains(suppressedCore, "scm-debug") != (goos.GOOS == "windows") {
		t.Errorf("the knob exists in Windows builds only: %v", suppressedCore)
	}
}

func TestSuppressUnknownFeatureIsCollected(t *testing.T) {
	old := suppressedCore
	t.Cleanup(func() { suppressedCore = old })
	before := defaultCollector.Len()
	Suppress(CoreFeature(99))
	if defaultCollector.Len() != before+1 {
		t.Error("an unknown feature must be recorded as a startup violation")
	}
}

func TestSuppressSourcesLeaveTheArgumentListAlone(t *testing.T) {
	oldCore, oldSources := suppressedCore, suppressedSources
	t.Cleanup(func() { suppressedCore, suppressedSources = oldCore, oldSources })
	suppressedCore, suppressedSources = nil, map[CoreFeature]bool{}
	Suppress(FeatureCompanionConfig, FeatureUserConfig, FeatureEnvironment)
	if len(suppressedCore) != 0 {
		t.Errorf("source features are not arguments: %v", suppressedCore)
	}
	locs := configLocations("mytool")
	if len(locs) != 1 || !strings.Contains(locs[0].Base, "mytool") || locs[0].Pinned {
		t.Errorf("only the system tier must remain: %+v", locs)
	}
	if environmentLookup() != nil {
		t.Error("a suppressed environment reads nothing")
	}
}

// Suppressing --config takes --upgrade-config and --write-config with
// it; an explicit repeat of either does not duplicate.
func TestSuppressConfigFileImpliesTheFileVerbs(t *testing.T) {
	old := suppressedCore
	t.Cleanup(func() { suppressedCore = old })
	suppressedCore = nil
	Suppress(FeatureConfigFile)
	Suppress(FeatureWriteConfig)
	want := []string{"config", "write-config", "upgrade-config", "from-version"}
	if !reflect.DeepEqual(suppressedCore, want) {
		t.Errorf("got %v, want %v", suppressedCore, want)
	}
}
