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
	"fmt"
	"reflect"

	"sxcli.dev/fw/conf"
)

// Metadata is the optional, declarative description of a service — the
// instance and its config struct are one service, so one Metadata
// covers both: a long-form Description of the service itself and
// per-field annotations for its configuration. Attach it at
// registration with the Metadata chain method; it is inert data with no methods to
// implement, validated at registration like everything else and served
// to meta consumers (completion, documentation) via the Introspector.
type Metadata struct {
	// Description is the long-form service/applet description; the
	// usage: tags remain the one-liners.
	Description string
	// Fields annotates config struct fields, keyed by Go field name
	// ("Level", "TLS.Cert" for nested). Every value must be a
	// FieldMetadata[T] instance; anything else, an unknown key, or
	// annotations on a service without a config struct are
	// registration violations.
	Fields map[string]any
}

// ValueHint is the advisory declaration of what a field's value
// denotes, for tooling (completion, documentation). Unlike Allowed a
// hint is never enforced — a hinted file may not exist yet (--config
// names the file --write-config is about to create); it is data in the
// same trust class as Doc. The core's own --config declares HintFile.
type ValueHint int

const (
	// HintNone declares nothing; the zero value.
	HintNone ValueHint = ValueHint(conf.HintNone)
	// HintFile: the value names a file, existing or to be created.
	HintFile ValueHint = ValueHint(conf.HintFile)
	// HintDirectory: the value names a directory.
	HintDirectory ValueHint = ValueHint(conf.HintDirectory)
	// HintServiceID: the value names a service registered in this
	// binary — completable from the Introspector. The core's own
	// --disable and --enable declare it.
	HintServiceID ValueHint = ValueHint(conf.HintServiceID)
)

// FieldMetadata annotates one config struct field. T carries the
// allowed values in the field's own type: T must have the same kind as
// (and be convertible to) the annotated field's type — for slice
// fields, the element type. A mismatch is a registration violation.
type FieldMetadata[T any] struct {
	// Allowed enumerates the complete value domain. Non-empty means
	// the domain is closed: the framework rejects any other value from
	// any source at startup, and completion services can offer it.
	Allowed []T
	// Doc is the long-form field description; usage: stays the
	// one-liner.
	Doc string
	// Hint declares what the value denotes. Valid only on string
	// fields (paths are strings; element type for slices) and mutually
	// exclusive with a non-empty Allowed — a closed enum and "it's a
	// file" contradict each other.
	Hint ValueHint
}

// fieldMetadataMarker identifies FieldMetadata instantiations across
// their type parameters.
type fieldMetadataMarker interface{ fieldMetadata() }

func (FieldMetadata[T]) fieldMetadata() {}

// defaultDomainViolations checks that a field's registered default —
// the value the struct holds at registration — is itself inside the
// declared domain: a default outside its own enum would be the first
// lie the enforcement catches, so it is caught at registration instead.
func defaultDomainViolations(serviceID, name string, allowed []any, probe conf.ProbedField) []error {
	var errs []error
	if len(allowed) > 0 {
		if probe.IsSlice {
			for i := 0; i < probe.Value.Len(); i++ {
				if !metaHas(allowed, probe.Value.Index(i).Interface()) {
					errs = append(errs, fmt.Errorf("service %q metadata: %q default element %v is not among the allowed values %v", serviceID, name, probe.Value.Index(i).Interface(), allowed))
				}
			}
		} else if !metaHas(allowed, probe.Value.Interface()) {
			errs = append(errs, fmt.Errorf("service %q metadata: %q default %v is not among the allowed values %v", serviceID, name, probe.Value.Interface(), allowed))
		}
	}
	return errs
}

func metaHas(allowed []any, v any) bool {
	out := false
	for _, a := range allowed {
		out = out || a == v
	}
	return out
}

// normalizeMetadata validates a registration's Metadata against the
// probed config fields and converts it into the internal
// representation the schema machinery consumes. The type-level checks
// always run; the value-level default-in-domain check runs only when
// withValues (the probes carry real field values — an instance
// exists). Shared by the old instance-based registry check and the
// catalog's commit path, which validates against ProbeType with no
// instance and defers the value check to Build.
func normalizeMetadata(id string, raw *Metadata, hasConfig bool, probes map[string]conf.ProbedField, withValues bool) (*conf.Meta, []error) {
	meta := &conf.Meta{Description: raw.Description, Fields: map[string]conf.FieldMeta{}}
	var errs []error
	if len(raw.Fields) > 0 && !hasConfig {
		errs = append(errs, fmt.Errorf("service %q: field metadata without a config struct", id))
	} else {
		for name, value := range raw.Fields {
			probe, known := probes[name]
			if !known {
				errs = append(errs, fmt.Errorf("service %q metadata: %q names no config field", id, name))
			} else if _, isFieldMetadata := value.(fieldMetadataMarker); !isFieldMetadata {
				errs = append(errs, fmt.Errorf("service %q metadata: %q must be a FieldMetadata value, got %T", id, name, value))
			} else {
				rv := reflect.ValueOf(value)
				allowedValues := rv.FieldByName("Allowed")
				elemType := allowedValues.Type().Elem()
				hint := ValueHint(rv.FieldByName("Hint").Int())
				if allowedValues.Len() > 0 && (elemType.Kind() != probe.Type.Kind() || !elemType.ConvertibleTo(probe.Type)) {
					errs = append(errs, fmt.Errorf("service %q metadata: %q allows %s values but the field takes %s", id, name, elemType, probe.Type))
				} else if hint < HintNone || hint > HintServiceID {
					errs = append(errs, fmt.Errorf("service %q metadata: %q declares an unknown hint %d", id, name, hint))
				} else if hint != HintNone && allowedValues.Len() > 0 {
					errs = append(errs, fmt.Errorf("service %q metadata: %q declares both a hint and an Allowed domain — a closed enum and a hint contradict each other", id, name))
				} else if hint != HintNone && probe.Type.Kind() != reflect.String {
					errs = append(errs, fmt.Errorf("service %q metadata: %q declares a hint but the field takes %s, not a string", id, name, probe.Type))
				} else {
					fm := conf.FieldMeta{Doc: rv.FieldByName("Doc").String(), Hint: conf.ValueHint(hint)}
					for i := 0; i < allowedValues.Len(); i++ {
						fm.Allowed = append(fm.Allowed, allowedValues.Index(i).Convert(probe.Type).Interface())
					}
					if withValues {
						errs = append(errs, defaultDomainViolations(id, name, fm.Allowed, probe)...)
					}
					meta.Fields[name] = fm
				}
			}
		}
	}
	return meta, errs
}
