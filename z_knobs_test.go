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
	"sort"
	"strings"
	"testing"

	"sxcli.dev/conf/engine"
	"sxcli.dev/fw/internal/ctlhook"
	"sxcli.dev/rules/solver"
)

// confTags reads a struct's conf tags into long → short.
func confTags(t reflect.Type, into map[string]string) {
	for i := 0; i < t.NumField(); i++ {
		if tag, has := t.Field(i).Tag.Lookup("conf"); has {
			long, short, _ := strings.Cut(tag, ",")
			into[long] = short
		}
	}
}

// The core's argument surface is declared twice: as struct tags here
// (the engine's core, the tool knobs, the shared-file verb, the
// Windows service debug knob, the controls) and as data in the shared
// rules' table, for tooling with no struct to reflect on. The two must
// say the same.
func TestCoreKnobsMatchTheSharedTable(t *testing.T) {
	got := map[string]string{}
	confTags(reflect.TypeOf(engine.Core{}), got)
	confTags(reflect.TypeOf(upgradeKnobs{}), got)
	confTags(reflect.TypeOf(listingKnob{}), got)
	confTags(reflect.TypeOf(sharedKnob{}), got)
	confTags(reflect.TypeOf(scmDebugKnob{}), got)
	confTags(reflect.TypeOf(ctlhook.Registered.New()).Elem(), got) // this test binary imports the controls
	want := map[string]string{}
	for _, k := range solver.Knobs {
		want[k.Long] = k.Short
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("struct tags %v, table %v", got, want)
	}
}

// Every feature the table names for a framework knob is one of this
// package's constants (the controls' are their package's), and the
// name resolves to that knob.
func TestFeatureNamesCoverTheSharedTable(t *testing.T) {
	byName := map[string]CoreFeature{}
	for feature, name := range coreFeatureNames {
		byName[name] = feature
	}
	for _, k := range solver.Knobs {
		if k.When != solver.KnobControls {
			feature, named := byName[k.Feature]
			if !named {
				t.Errorf("%s: the table's feature %s is not a fw constant", k.Long, k.Feature)
			} else if longs, _ := featureLongs(feature); k.When != solver.KnobSharedFile && !k.Windows && !contains(longs, k.Long) {
				t.Errorf("%s does not resolve to --%s: %v", k.Feature, k.Long, longs)
			}
		}
	}
}

// The controls' features resolve through their package's registration.
func TestControlFeaturesResolveThroughTheHook(t *testing.T) {
	var got []string
	for feature := range ctlhook.Registered.FeatureLongs {
		longs, known := featureLongs(CoreFeature(feature))
		if !known || len(longs) != 1 {
			t.Errorf("control feature %d: %v %v", feature, longs, known)
		} else {
			got = append(got, longs[0])
		}
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, []string{"disable", "enable", "override"}) {
		t.Errorf("%v", got)
	}
}
