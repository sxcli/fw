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

package config

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// setFromString converts one environment- or argument-sourced string
// into a scalar field. Durations are strict: a unit suffix is required
// ("5s", "5000ms", "5000000ns"; bare numbers other than "0" are
// rejected by time.ParseDuration).
func setFromString(f reflect.Value, s string) error {
	var err error
	if f.Type() == durationType {
		if d, perr := time.ParseDuration(s); perr == nil {
			f.SetInt(int64(d))
		} else {
			err = fmt.Errorf("invalid duration %q: a unit suffix is required (e.g. 5s, 5000ms)", s)
		}
	} else {
		switch f.Kind() {
		case reflect.String:
			f.SetString(s)
		case reflect.Bool:
			if b, perr := strconv.ParseBool(s); perr == nil {
				f.SetBool(b)
			} else {
				err = fmt.Errorf("invalid bool %q", s)
			}
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if n, perr := strconv.ParseInt(s, 10, f.Type().Bits()); perr == nil {
				f.SetInt(n)
			} else {
				err = fmt.Errorf("invalid integer %q", s)
			}
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if n, perr := strconv.ParseUint(s, 10, f.Type().Bits()); perr == nil {
				f.SetUint(n)
			} else {
				err = fmt.Errorf("invalid unsigned integer %q", s)
			}
		case reflect.Float32, reflect.Float64:
			if n, perr := strconv.ParseFloat(s, f.Type().Bits()); perr == nil {
				f.SetFloat(n)
			} else {
				err = fmt.Errorf("invalid number %q", s)
			}
		default:
			err = fmt.Errorf("unsupported field kind %s", f.Kind())
		}
	}
	return err
}

// setFromJSON converts one config-file value into a field. The same
// strictness as setFromString applies: durations must be strings with a
// unit suffix, never bare numbers.
func setFromJSON(f reflect.Value, raw json.RawMessage) error {
	var err error
	if f.Kind() == reflect.Slice {
		var elems []json.RawMessage
		if err = json.Unmarshal(raw, &elems); err == nil {
			s := reflect.MakeSlice(f.Type(), 0, len(elems))
			for _, e := range elems {
				el := reflect.New(f.Type().Elem()).Elem()
				if serr := setFromJSON(el, e); serr == nil {
					s = reflect.Append(s, el)
				} else if err == nil {
					err = serr
				}
			}
			if err == nil {
				f.Set(s)
			}
		} else {
			err = fmt.Errorf("expected a json array: %v", err)
		}
	} else if f.Type() == durationType {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			err = setFromString(f, s)
		} else {
			err = fmt.Errorf("a duration must be a json string with a unit suffix (e.g. \"5s\"), got %s", raw)
		}
	} else {
		switch f.Kind() {
		case reflect.String:
			var s string
			if err = json.Unmarshal(raw, &s); err == nil {
				f.SetString(s)
			}
		case reflect.Bool:
			var b bool
			if err = json.Unmarshal(raw, &b); err == nil {
				f.SetBool(b)
			}
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64:
			var n json.Number
			if err = json.Unmarshal(raw, &n); err == nil {
				err = setFromString(f, n.String())
			}
		default:
			err = fmt.Errorf("unsupported field kind %s", f.Kind())
		}
	}
	return err
}

// setSliceFromEnv replaces a slice field with the comma-separated
// elements of one environment value. An empty value means an empty
// slice — there is no other way to express one from the environment.
func setSliceFromEnv(f reflect.Value, value string) error {
	var err error
	s := reflect.MakeSlice(f.Type(), 0, 4)
	if value != "" {
		for _, part := range strings.Split(value, ",") {
			el := reflect.New(f.Type().Elem()).Elem()
			if serr := setFromString(el, strings.TrimSpace(part)); serr == nil {
				s = reflect.Append(s, el)
			} else if err == nil {
				err = serr
			}
		}
	}
	if err == nil {
		f.Set(s)
	}
	return err
}

// appendFromString appends one argument-sourced element to a slice
// field.
func appendFromString(f reflect.Value, s string) error {
	el := reflect.New(f.Type().Elem()).Elem()
	err := setFromString(el, s)
	if err == nil {
		f.Set(reflect.Append(f, el))
	}
	return err
}

// fieldValue renders one field's current value for serialization:
// durations become unit-suffixed strings, nil slices become empty json
// arrays (never null), everything else marshals naturally.
func fieldValue(f reflect.Value) any {
	var out any
	if f.Type() == durationType {
		out = time.Duration(f.Int()).String()
	} else if f.Kind() == reflect.Slice && f.Type().Elem() == durationType {
		ds := []string{}
		for i := 0; i < f.Len(); i++ {
			ds = append(ds, time.Duration(f.Index(i).Int()).String())
		}
		out = ds
	} else if f.Kind() == reflect.Slice && f.IsNil() {
		out = []any{}
	} else {
		out = f.Interface()
	}
	return out
}

// MarshalIndent serializes the merged configuration of every schema
// member — the exact values the config structs hold — as the core's
// native JSON, service sections keyed by id.
func (s *Schema) MarshalIndent() ([]byte, error) {
	root := map[string]any{}
	for _, svc := range s.services {
		section := map[string]any{}
		for _, f := range svc.fields {
			if !f.Transient {
				node := section
				for _, key := range f.JSONPath[:len(f.JSONPath)-1] {
					if next, ok := node[key].(map[string]any); ok {
						node = next
					} else {
						created := map[string]any{}
						node[key] = created
						node = created
					}
				}
				node[f.JSONPath[len(f.JSONPath)-1]] = fieldValue(svc.cfg.Elem().FieldByIndex(f.Path))
			}
		}
		root[svc.id] = section
	}
	return json.MarshalIndent(root, "", "  ")
}
