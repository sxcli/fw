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
	"cmp"
	"fmt"
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
	// Min and Max are the lowest and highest value the field accepts,
	// both inclusive. A nil side has no bound. Bound builds them, so a
	// type without an order cannot have one. For a slice field they
	// apply to every element. A field has either Allowed or bounds,
	// not both.
	Min *T
	Max *T
	// Doc is the long-form field description; usage: stays the
	// one-liner.
	Doc string
	// Hint declares what the value denotes. Valid only on string
	// fields (paths are strings; element type for slices) and mutually
	// exclusive with a non-empty Allowed — a closed enum and "it's a
	// file" contradict each other.
	Hint ValueHint
}

// Bound returns a pointer to v for FieldMetadata's Min or Max. The
// type parameter is limited to cmp.Ordered, so a bool or a struct is
// refused by the compiler.
func Bound[T cmp.Ordered](v T) *T {
	return &v
}

// fieldMetadataMarker identifies FieldMetadata instantiations across
// their type parameters.
type fieldMetadataMarker interface{ fieldMetadata() }

func (FieldMetadata[T]) fieldMetadata() {}

// defaultDomainViolations checks that a field's factory default, the
// value the struct holds when the instance is built, is itself among
// its Allowed values or within its Min and Max. Otherwise the first
// run with no input would fail on the author's own default, so Build
// reports it.
func defaultDomainViolations(c *fail.Collector, service, name string, fm engine.FieldMeta, probe engine.ProbedField) {
	if probe.IsSlice {
		for i := 0; i < probe.Value.Len(); i++ {
			defaultViolation(c, service, name, fm, probe.Value.Index(i).Interface(), true)
		}
	} else {
		defaultViolation(c, service, name, fm, probe.Value.Interface(), false)
	}
}

// defaultViolation checks one default value, a scalar or one slice
// element, against the field's Allowed values or its Min and Max.
func defaultViolation(c *fail.Collector, service, name string, fm engine.FieldMeta, v any, element bool) {
	if len(fm.Allowed) > 0 {
		if !metaHas(fm.Allowed, v) {
			if element {
				c.Fail("service %s metadata: field %q has an invalid default value: %v. It can contain only: %s", service, name, v, tags.AllowedList(fm.Allowed))
			} else {
				c.Fail("service %s metadata: field %q has an invalid default value: %v. It can be one of: %s", service, name, v, tags.AllowedList(fm.Allowed))
			}
		}
	} else if fm.Min != nil && engine.Compare(v, fm.Min) < 0 {
		c.Fail("service %s metadata: field %q has an invalid default value: %v. It must be at least %v", service, name, v, fm.Min)
	} else if fm.Max != nil && engine.Compare(v, fm.Max) > 0 {
		c.Fail("service %s metadata: field %q has an invalid default value: %v. It must be at most %v", service, name, v, fm.Max)
	}
}

// metadataFits says whether a FieldMetadata's Allowed element type can
// annotate a field of the given type (the element type for slices):
// the same kind, and convertible to it. sxcli-vet answers the same
// question over go/types; sxcli.dev/rules/conformance holds the two
// to one matrix.
func metadataFits(allowed, field reflect.Type) bool {
	return allowed.Kind() == field.Kind() && allowed.ConvertibleTo(field)
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
func normalizeMetadata(c *fail.Collector, service string, raw *Metadata, hasConfig bool, probes map[string]engine.ProbedField, withValues bool) *engine.Meta {
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
			minV := rv.FieldByName("Min")
			maxV := rv.FieldByName("Max")
			if !minV.IsNil() {
				f.HasMin = true
				f.Min = fmt.Sprint(minV.Elem().Interface())
			}
			if !maxV.IsNil() {
				f.HasMax = true
				f.Max = fmt.Sprint(maxV.Elem().Interface())
			}
			// both bounds have the type T, so they are compared as T,
			// before any conversion to the field's type
			f.MinAboveMax = f.HasMin && f.HasMax && engine.Compare(minV.Elem().Interface(), maxV.Elem().Interface()) > 0
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
			p.Ordered = probed.Type.Kind() != reflect.Bool
			if rv, isField := values[f.Name]; isField {
				p.Fits = metadataFits(rv.FieldByName("Allowed").Type().Elem(), probed.Type)
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
			c.Fail("service %s: %s", service, body)
		} else {
			c.Fail("service %s %s", service, body)
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
		if minV := rv.FieldByName("Min"); !minV.IsNil() {
			fm.Min = minV.Elem().Convert(probed.Type).Interface()
		}
		if maxV := rv.FieldByName("Max"); !maxV.IsNil() {
			fm.Max = maxV.Elem().Convert(probed.Type).Interface()
		}
		if withValues {
			defaultDomainViolations(c, service, name, fm, probed)
		}
		meta.Fields[name] = fm
	}
	return meta
}
