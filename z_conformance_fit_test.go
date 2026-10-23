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

// The runtime's half of the metadata fit matrix in
// sxcli.dev/rules/conformance: every FitCandidates type against every
// FitFields field, decided by metadataFits over reflect. This side
// WRITES golden_fit.txt with -update-conformance; vet's half must
// match it.
package fw

import (
	"flag"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"sxcli.dev/rules/conformance"
)

var updateConformance = flag.Bool("update-conformance", false, "rewrite the conformance fit golden from this run")

func renderFitMatrix() string {
	fields := reflect.TypeOf(conformance.FitFields{})
	candidates := reflect.TypeOf(conformance.FitCandidates{})
	var fits []conformance.Fit
	for i := 0; i < fields.NumField(); i++ {
		field := fields.Field(i)
		target := field.Type
		if target.Kind() == reflect.Slice {
			target = target.Elem() // a FieldMetadata annotates a slice field's elements
		}
		for j := 0; j < candidates.NumField(); j++ {
			candidate := candidates.Field(j)
			fits = append(fits, conformance.Fit{Field: field.Name, Candidate: candidate.Name, Fits: metadataFits(candidate.Type, target)})
		}
	}
	return conformance.RenderFits(fits)
}

func TestMetadataFitGolden(t *testing.T) {
	got := renderFitMatrix()
	if *updateConformance {
		if err := os.WriteFile(conformance.FitGoldenPath(), []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	} else if got != conformance.FitGolden {
		want := strings.Split(conformance.FitGolden, "\n")
		lines := strings.Split(got, "\n")
		for i := 0; i < len(want) || i < len(lines); i++ {
			w, g := "", ""
			if i < len(want) {
				w = want[i]
			}
			if i < len(lines) {
				g = lines[i]
			}
			if w != g {
				t.Fatalf("the runtime's fit matrix differs from golden_fit.txt at line %s; rerun with -update-conformance if the runtime is right\n  golden: %s\n  got:    %s", strconv.Itoa(i+1), w, g)
			}
		}
	}
}
