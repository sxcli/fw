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

package sxclifw

import (
	"reflect"
	"strings"
	"testing"
)

func TestSuppressMapsFeaturesToLongNames(t *testing.T) {
	old := suppressedCore
	t.Cleanup(func() { suppressedCore = old })
	suppressedCore = nil
	Suppress(FeatureConfigFile, FeatureWriteConfig, FeatureDisable, FeatureEnable, FeatureOverride, FeatureHelp)
	want := []string{"config", "write-config", "disable", "enable", "override", "help"}
	if !reflect.DeepEqual(suppressedCore, want) {
		t.Errorf("got %v, want %v", suppressedCore, want)
	}
}

func TestSuppressDeduplicates(t *testing.T) {
	old := suppressedCore
	t.Cleanup(func() { suppressedCore = old })
	suppressedCore = nil
	Suppress(FeatureOverride)
	Suppress(FeatureOverride, FeatureOverride)
	if !reflect.DeepEqual(suppressedCore, []string{"override"}) {
		t.Errorf("repeated suppression must not duplicate: %v", suppressedCore)
	}
}

func TestMaxConfigSize(t *testing.T) {
	old := maxConfigSize
	t.Cleanup(func() { maxConfigSize = old })
	MaxConfigSize(4096)
	if maxConfigSize != 4096 {
		t.Errorf("limit not set: %d", maxConfigSize)
	}
	before := defaultCollector.Len()
	MaxConfigSize(0)
	if defaultCollector.Len() != before+1 {
		t.Error("a non-positive limit must be a violation")
	}
	if maxConfigSize != 4096 {
		t.Error("a rejected limit must not overwrite the previous one")
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
	Enable(FeatureOverride)
	if defaultCollector.Len() != before+1 {
		t.Error("enabling a default-on feature must be a violation")
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
