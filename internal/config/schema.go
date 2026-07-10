package config

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/sxcli/sxcli-fw/internal/fail"
	"github.com/sxcli/sxcli-fw/internal/registry"
)

var durationType = reflect.TypeOf(time.Duration(0))

// ValidateConfig is a registry.Check validating the tags and field types
// of a service's config struct at registration time.
func ValidateConfig(d *registry.Descriptor) error {
	var err error
	if d.ConfigPtr != nil {
		if _, errs := extract(d.ID, reflect.TypeOf(d.ConfigPtr).Elem(), nil, nil, "", true); len(errs) > 0 {
			err = errors.Join(errs...)
		}
	}
	return err
}

// NewSchema builds the full schema of one invocation: the core config
// first (so its short forms win), then every member owning a config
// struct. Core fields whose long name appears in suppress are removed
// from the schema entirely: the argument becomes unknown, the env var is
// never consulted, and the file key turns into an unknown-key violation.
// Duplicate long argument names and duplicate explicit env names across
// the schema are violations; short-form collisions are resolved
// first-come-first-served.
func NewSchema(c *fail.Collector, appletID string, core *Core, members []*registry.Descriptor, suppress []string) *Schema {
	s := &Schema{
		appletID: appletID,
		long:     map[string]*Field{},
		short:    map[string]*Field{},
		owner:    map[*Field]*serviceSchema{},
	}
	s.add(c, "core", reflect.ValueOf(core), suppress)
	for _, d := range members {
		if d.ConfigPtr != nil {
			s.add(c, d.ID, reflect.ValueOf(d.ConfigPtr), nil)
		}
	}
	env := map[string]*Field{}
	for _, svc := range s.services {
		for _, f := range svc.fields {
			if f.Long != "" {
				if prev, dup := s.long[f.Long]; !dup {
					s.long[f.Long] = f
				} else {
					c.Fail("duplicate argument --%s between service %q and service %q", f.Long, prev.ServiceID, f.ServiceID)
				}
				if f.Short != "" {
					if _, taken := s.short[f.Short]; !taken {
						s.short[f.Short] = f
					} else {
						f.Short = "" // first come, first served
					}
				}
				if f.EnvName == "" {
					f.EnvName = strings.ToUpper(appletID) + "_" + strings.ToUpper(strings.ReplaceAll(f.Long, "-", "_"))
				}
			}
			if f.EnvName != "" {
				if prev, dup := env[f.EnvName]; !dup {
					env[f.EnvName] = f
				} else {
					c.Fail("duplicate environment variable %s between service %q and service %q", f.EnvName, prev.ServiceID, f.ServiceID)
				}
			}
		}
	}
	return s
}

func (s *Schema) add(c *fail.Collector, id string, cfgPtr reflect.Value, suppress []string) {
	fields, errs := extract(id, cfgPtr.Type().Elem(), nil, nil, "", true)
	for _, err := range errs {
		c.Add(err)
	}
	if len(suppress) > 0 {
		drop := map[string]bool{}
		for _, long := range suppress {
			drop[long] = true
		}
		var kept []*Field
		for _, f := range fields {
			if drop[f.Long] {
				delete(drop, f.Long)
			} else {
				kept = append(kept, f)
			}
		}
		for long := range drop {
			c.Fail("suppress: %q does not name a core argument", long)
		}
		fields = kept
	}
	svc := &serviceSchema{id: id, cfg: cfgPtr, fields: fields}
	s.services = append(s.services, svc)
	for _, f := range fields {
		s.owner[f] = svc
	}
}

// extract walks one config struct type and returns its settable fields.
// Nested structs are file-only: arg and env tags below the top level are
// errors, as are embedded fields, unsupported field types and missing or
// duplicate json names.
func extract(serviceID string, t reflect.Type, path []int, jsonPath []string, namePrefix string, topLevel bool) ([]*Field, []error) {
	var fields []*Field
	var errs []error
	longs := map[string]bool{}
	shorts := map[string]bool{}
	jsonNames := map[string]bool{}
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.IsExported() {
			name := namePrefix + sf.Name
			if sf.Anonymous {
				errs = append(errs, fmt.Errorf("service %q config field %s: embedded fields are not supported", serviceID, name))
			} else {
				f := &Field{
					ServiceID: serviceID,
					Name:      name,
					Path:      append(append([]int{}, path...), i),
					Type:      sf.Type,
					Usage:     sf.Tag.Get("usage"),
				}
				jsonName, hasJSON := sf.Tag.Lookup("json")
				jsonName, _, _ = strings.Cut(jsonName, ",")
				if hasJSON && jsonName != "" && jsonName != "-" {
					if !jsonNames[jsonName] {
						jsonNames[jsonName] = true
						f.JSONPath = append(append([]string{}, jsonPath...), jsonName)
					} else {
						errs = append(errs, fmt.Errorf("service %q config field %s: duplicate json name %q", serviceID, name, jsonName))
					}
				} else {
					errs = append(errs, fmt.Errorf("service %q config field %s: a json tag with a name is required", serviceID, name))
				}
				if arg, hasArg := sf.Tag.Lookup("arg"); hasArg {
					long, short, hasShort := strings.Cut(arg, ",")
					if isValidLong(long) && (!hasShort || isValidShort(short)) {
						if !longs[long] && (!hasShort || !shorts[short]) {
							longs[long] = true
							f.Long = long
							if hasShort {
								shorts[short] = true
								f.Short = short
							}
						} else {
							errs = append(errs, fmt.Errorf("service %q config field %s: duplicate argument name in %q", serviceID, name, arg))
						}
					} else {
						errs = append(errs, fmt.Errorf("service %q config field %s: invalid arg tag %q", serviceID, name, arg))
					}
				}
				if env, hasEnv := sf.Tag.Lookup("env"); hasEnv {
					if isValidEnv(env) {
						f.EnvName = env
					} else {
						errs = append(errs, fmt.Errorf("service %q config field %s: invalid env tag %q", serviceID, name, env))
					}
				}
				if !topLevel && (f.Long != "" || f.EnvName != "") {
					errs = append(errs, fmt.Errorf("service %q config field %s: nested struct fields are file-only, arg/env tags are not allowed", serviceID, name))
				}
				// A field without a valid json name is unusable — the
				// violation is already recorded; keeping it would leave
				// a nil JSONPath for applyObject/MarshalIndent to trip
				// over.
				if len(f.JSONPath) > 0 {
					if sf.Type.Kind() == reflect.Struct {
						if f.Long == "" && f.EnvName == "" {
							nested, nerrs := extract(serviceID, sf.Type, f.Path, f.JSONPath, name+".", false)
							fields = append(fields, nested...)
							errs = append(errs, nerrs...)
						} else {
							errs = append(errs, fmt.Errorf("service %q config field %s: struct fields cannot carry arg/env tags", serviceID, name))
						}
					} else if sf.Type.Kind() == reflect.Slice {
						if scalarOK(sf.Type.Elem()) {
							f.IsSlice = true
							fields = append(fields, f)
						} else {
							errs = append(errs, fmt.Errorf("service %q config field %s: unsupported slice element type %s", serviceID, name, sf.Type.Elem()))
						}
					} else if scalarOK(sf.Type) {
						fields = append(fields, f)
					} else {
						errs = append(errs, fmt.Errorf("service %q config field %s: unsupported type %s", serviceID, name, sf.Type))
					}
				}
			}
		}
	}
	return fields, errs
}

// scalarOK reports whether t is a supported scalar: string, bool, any
// int/uint width, any float width or time.Duration.
func scalarOK(t reflect.Type) bool {
	var ok bool
	switch t.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		ok = true
	}
	return ok
}

// isValidLong reports whether long is a valid long argument name:
// lowercase, at least two characters, letters/digits/dashes, starting
// with a letter and not ending with a dash.
func isValidLong(long string) bool {
	valid := len(long) >= 2 && long[0] >= 'a' && long[0] <= 'z' && long[len(long)-1] != '-'
	for _, c := range long {
		valid = valid && ('a' <= c && c <= 'z' || '0' <= c && c <= '9' || c == '-')
	}
	return valid
}

// isValidShort reports whether short is a valid single-character short
// form.
func isValidShort(short string) bool {
	return len(short) == 1 && (short[0] >= 'a' && short[0] <= 'z' || short[0] >= '0' && short[0] <= '9')
}

// isValidEnv reports whether env is a valid environment variable name:
// uppercase letters, digits and underscores, not starting with a digit.
func isValidEnv(env string) bool {
	valid := env != "" && !(env[0] >= '0' && env[0] <= '9')
	for _, c := range env {
		valid = valid && ('A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '_')
	}
	return valid
}
