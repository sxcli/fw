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
	"strings"
	"testing"

	"sxcli.dev/conf/engine"
)

func TestSuppressMapsFeaturesToLongNames(t *testing.T) {
	old := suppressedCore
	t.Cleanup(func() { suppressedCore = old })
	suppressedCore = nil
	Suppress(FeatureConfigFile, FeatureWriteConfig, FeatureValidateConfig, FeatureHelp)
	want := []string{"config", "write-config", "validate-config", "help"}
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

func TestEnableSCMDebug(t *testing.T) {
	old := scmDebugEnabled
	t.Cleanup(func() { scmDebugEnabled = old })
	scmDebugEnabled = false
	Enable(FeatureSCMDebug)
	if !scmDebugEnabled {
		t.Error("Enable(FeatureSCMDebug) must set the opt-in")
	}
}

func TestEnableDefaultOnFeatureIsViolation(t *testing.T) {
	before := defaultCollector.Len()
	Enable(FeatureHelp)
	if defaultCollector.Len() != before+1 {
		t.Error("enabling a default-on feature must be a violation")
	}
}

func TestControlsAreOffByDefault(t *testing.T) {
	// the three controls are default-off: they ride the effective
	// suppression until the author enables them, and Suppress refuses
	// them like every default-off feature
	all := strings.Join(effectiveSuppressedCore(), ",")
	for _, long := range []string{"disable", "enable", "override"} {
		if !strings.Contains(all, long) {
			t.Errorf("%s must be off by default: %v", long, all)
		}
	}
	old := enabledControls
	t.Cleanup(func() { enabledControls = old })
	enabledControls = map[CoreFeature]bool{}
	Enable(FeatureDisable)
	if strings.Contains(strings.Join(effectiveSuppressedCore(), ","), "disable") {
		t.Error("an enabled control must leave the suppression")
	}
	before := defaultCollector.Len()
	Suppress(FeatureEnable)
	if defaultCollector.Len() != before+1 {
		t.Error("suppressing a default-off control must be a violation")
	}
}

func TestSuppressSCMDebugIsViolation(t *testing.T) {
	before := defaultCollector.Len()
	Suppress(FeatureSCMDebug)
	if defaultCollector.Len() != before+1 {
		t.Error("suppressing the default-off feature must be a violation")
	}
}

func TestStripSCMDebug(t *testing.T) {
	argv, found := stripSCMDebug([]string{"bin", "--verbose", "--scm-debug", "trailing"})
	if !found || strings.Join(argv, ",") != "bin,--verbose,trailing" {
		t.Errorf("strip wrong: %v, %v", argv, found)
	}
	argv, found = stripSCMDebug([]string{"--scm-debug"})
	if found || strings.Join(argv, ",") != "--scm-debug" {
		t.Errorf("argv[0] must never be a candidate: %v, %v", argv, found)
	}
	if _, found = stripSCMDebug([]string{"bin", "run"}); found {
		t.Error("absent token must not be found")
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
