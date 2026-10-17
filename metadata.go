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
	"sxcli.dev/fw/system"
	"sxcli.dev/rules/registration"
	"sxcli.dev/rules/tags"

	"sxcli.dev/conf/engine"
	"sxcli.dev/conf/fail"
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

// ValueHint and the hint constants are the system vocabulary,
// re-exported: one home (the engine defines the universal set, the
// system package speaks for fw's own).
type ValueHint = system.ValueHint

const (
	HintNone      = system.HintNone
	HintFile      = system.HintFile
	HintDirectory = system.HintDirectory
	HintServiceID = system.HintServiceID
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
func defaultDomainViolations(c *fail.Collector, serviceID, name string, allowed []any, probe engine.ProbedField) {
	if len(allowed) > 0 {
		if probe.IsSlice {
			for i := 0; i < probe.Value.Len(); i++ {
				if !metaHas(allowed, probe.Value.Index(i).Interface()) {
					c.Fail("service %q metadata: %q default element %v is not among the allowed values %v", serviceID, name, probe.Value.Index(i).Interface(), allowed)
				}
			}
		} else if !metaHas(allowed, probe.Value.Interface()) {
			c.Fail("service %q metadata: %q default %v is not among the allowed values %v", serviceID, name, probe.Value.Interface(), allowed)
		}
	}
}

func metaHas(allowed []any, v any) bool {
	out := false
	for _, a := range allowed {
		out = out || a == v
	}
	return out
}

// normalizeMetadata validates a registration's Metadata against the
// probed config fields — the verdicts are the shared rules'
// (registration.CheckMetadata), this side reads the values and
// answers the type questions — and converts the clean entries into
// the internal representation the schema machinery consumes. The
// type-level checks always run; the value-level default-in-domain
// check runs only when withValues (the probes carry real field values
// — an instance exists). Called from the catalog's commit path, which
// validates against ProbeType with no instance and defers the value
// check to Build (defaultsInDomain).
func normalizeMetadata(c *fail.Collector, id string, raw *Metadata, hasConfig bool, probes map[string]engine.ProbedField, withValues bool) *engine.Meta {
	meta := &engine.Meta{Description: raw.Description, Fields: map[string]engine.FieldMeta{}}
	var fields []registration.MetaField
	values := map[string]reflect.Value{} // the FieldMetadata values by key
	for name, value := range raw.Fields {
		f := registration.MetaField{Name: name}
		if _, isFieldMetadata := value.(fieldMetadataMarker); isFieldMetadata {
			rv := reflect.ValueOf(value)
			values[name] = rv
			f.IsField = true
			f.Allowed = rv.FieldByName("Allowed").Len()
			f.AllowedT = tags.TypeID(rv.FieldByName("Allowed").Type().Elem())
			f.Hint = int(rv.FieldByName("Hint").Int())
		} else {
			f.Got = reflect.TypeOf(value).String()
		}
		fields = append(fields, f)
	}
	probe := func(f registration.MetaField) (registration.MetaProbe, bool) {
		var p registration.MetaProbe
		probed, known := probes[f.Name]
		if known {
			p.Type = tags.TypeID(probed.Type)
			p.String = probed.Type.Kind() == reflect.String
			if rv, isField := values[f.Name]; isField {
				elemType := rv.FieldByName("Allowed").Type().Elem()
				p.Fits = elemType.Kind() == probed.Type.Kind() && elemType.ConvertibleTo(probed.Type)
			}
		}
		return p, known
	}
	knownHint := func(h int) bool {
		hint := ValueHint(h)
		return hint == HintNone || hint == HintFile || hint == HintDirectory || hint == HintServiceID
	}
	clean, bodies := registration.CheckMetadata(hasConfig, fields, probe, knownHint)
	for _, body := range bodies {
		if body == registration.MetaNoConfigRule {
			c.Fail("service %q: %s", id, body)
		} else {
			c.Fail("service %q %s", id, body)
		}
	}
	for _, name := range clean {
		rv := values[name]
		probed := probes[name]
		allowedValues := rv.FieldByName("Allowed")
		fm := engine.FieldMeta{Doc: rv.FieldByName("Doc").String(), Hint: ValueHint(rv.FieldByName("Hint").Int())}
		for i := 0; i < allowedValues.Len(); i++ {
			fm.Allowed = append(fm.Allowed, allowedValues.Index(i).Convert(probed.Type).Interface())
		}
		if withValues {
			defaultDomainViolations(c, id, name, fm.Allowed, probed)
		}
		meta.Fields[name] = fm
	}
	return meta
}
