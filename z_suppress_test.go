package sxclifw

import (
	"reflect"
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

func TestSuppressUnknownFeatureIsCollected(t *testing.T) {
	old := suppressedCore
	t.Cleanup(func() { suppressedCore = old })
	before := defaultCollector.Len()
	Suppress(CoreFeature(99))
	if defaultCollector.Len() != before+1 {
		t.Error("an unknown feature must be recorded as a startup violation")
	}
}
