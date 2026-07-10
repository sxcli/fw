package registry

import (
	"fmt"
	"reflect"
	"strings"

	"sxcli.dev/fw/internal/fail"
)

// New creates an empty registry recording violations into c and running
// the given semantic checks against every registration.
func New(c *fail.Collector, checks ...Check) *Registry {
	return &Registry{
		c:        c,
		checks:   checks,
		byID:     map[string]*Descriptor{},
		concrete: map[reflect.Type]string{},
	}
}

// fail records a registration violation.
func (r *Registry) fail(format string, args ...any) {
	r.c.Fail(format, args...)
}

// Register validates the instance and options and stores a descriptor.
// Violations that leave the service identity intact (bad interface
// declaration, bad config pointer, bad inject tag) are recorded while the
// descriptor is still stored; violations of identity itself (bad id,
// duplicate id, bad instance, duplicate concrete type) discard the
// registration.
func (r *Registry) Register(id string, instance any, opts Options) {
	if isValidID(id) {
		if _, dup := r.byID[id]; !dup {
			t := reflect.TypeOf(instance)
			if instance != nil && t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Struct && !reflect.ValueOf(instance).IsNil() {
				if prev, taken := r.concrete[t]; !taken {
					d := &Descriptor{ID: id, Instance: instance, Concrete: t}
					r.validateProvides(d, opts.Interfaces)
					r.validateConfig(d, opts.Config)
					r.collectDeps(d)
					for _, check := range r.checks {
						if err := check(d); err != nil {
							r.c.Add(err)
						}
					}
					r.ordered = append(r.ordered, d)
					r.byID[id] = d
					r.concrete[t] = id
				} else {
					r.fail("service %q: concrete type %s is already registered as %q", id, t, prev)
				}
			} else {
				r.fail("service %q: instance must be a non-nil pointer to struct", id)
			}
		} else {
			r.fail("service %q: duplicate id", id)
		}
	} else {
		r.fail("service id %q: must be a lowercase go identifier", id)
	}
}

// ByID returns the descriptor registered under id.
func (r *Registry) ByID(id string) (*Descriptor, bool) {
	d, ok := r.byID[id]
	return d, ok
}

// All returns every stored descriptor in registration order. The order
// is semantic: single-valued dependencies take the first match and slice
// dependencies preserve it.
func (r *Registry) All() []*Descriptor {
	return r.ordered
}

// Retain drops every descriptor whose id is not in keep, so the
// instances of services outside the resolved closure can be garbage
// collected (best effort: a package-level reference kept by the
// registering package defeats it). The composition is fixed once
// resolved — ejected services cannot come back.
func (r *Registry) Retain(keep map[string]bool) {
	var kept []*Descriptor
	for _, d := range r.ordered {
		if keep[d.ID] {
			kept = append(kept, d)
		} else {
			delete(r.byID, d.ID)
			delete(r.concrete, d.Concrete)
		}
	}
	r.ordered = kept
}

func (r *Registry) validateProvides(d *Descriptor, declared []reflect.Type) {
	for _, it := range declared {
		if it != nil && it.Kind() == reflect.Interface {
			if d.Concrete.Implements(it) {
				d.Provides = append(d.Provides, it)
			} else {
				r.fail("service %q: %s does not implement declared interface %s", d.ID, d.Concrete, it)
			}
		} else {
			r.fail("service %q: Provides declares non-interface type %v", d.ID, it)
		}
	}
}

func (r *Registry) validateConfig(d *Descriptor, cfg any) {
	if cfg != nil {
		t := reflect.TypeOf(cfg)
		if t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Struct && !reflect.ValueOf(cfg).IsNil() {
			d.ConfigPtr = cfg
		} else {
			r.fail("service %q: config must be a non-nil pointer to struct", d.ID)
		}
	}
}

func (r *Registry) collectDeps(d *Descriptor) {
	for _, f := range reflect.VisibleFields(d.Concrete.Elem()) {
		if tag, tagged := f.Tag.Lookup("inject"); tagged {
			if f.IsExported() {
				if ids, optional, err := parseInjectTag(tag); err == nil {
					dep := DepField{Index: f.Index, Name: f.Name, IDs: ids, Optional: optional}
					if f.Type.Kind() == reflect.Slice {
						if f.Type.Elem().Kind() == reflect.Interface {
							dep.IsSlice = true
							dep.Type = f.Type.Elem()
							d.Deps = append(d.Deps, dep)
						} else {
							r.fail("service %q field %s: inject slices carry interfaces only (concrete types are unique)", d.ID, f.Name)
						}
					} else if f.Type.Kind() == reflect.Interface || f.Type.Kind() == reflect.Pointer && f.Type.Elem().Kind() == reflect.Struct {
						if len(ids) <= 1 {
							dep.Type = f.Type
							d.Deps = append(d.Deps, dep)
						} else {
							r.fail("service %q field %s: a single-valued inject field may name at most one id", d.ID, f.Name)
						}
					} else {
						r.fail("service %q field %s: inject fields must be an interface, a pointer to struct, or a slice of interface", d.ID, f.Name)
					}
				} else {
					r.fail("service %q field %s: %v", d.ID, f.Name, err)
				}
			} else {
				r.fail("service %q field %s: inject tag on unexported field", d.ID, f.Name)
			}
		}
	}
}

// parseInjectTag parses the `inject` tag grammar
// "<id>[,<id>...][;optional]".
func parseInjectTag(tag string) ([]string, bool, error) {
	var ids []string
	var optional bool
	var err error
	idPart := tag
	if i := strings.IndexByte(tag, ';'); i >= 0 {
		idPart = tag[:i]
		if flag := tag[i+1:]; flag == "optional" {
			optional = true
		} else {
			err = fmt.Errorf("unknown inject flag %q", flag)
		}
	}
	if err == nil && idPart != "" {
		for _, raw := range strings.Split(idPart, ",") {
			id := strings.TrimSpace(raw)
			if isValidID(id) {
				ids = append(ids, id)
			} else if err == nil {
				err = fmt.Errorf("invalid service id %q in inject tag", id)
			}
		}
	}
	return ids, optional, err
}

// isValidID reports whether id is a non-empty, all-lowercase go
// identifier (the blank identifier "_" is not a service id).
func isValidID(id string) bool {
	valid := id != "" && id != "_"
	for i, c := range id {
		valid = valid && (c == '_' || 'a' <= c && c <= 'z' || i > 0 && '0' <= c && c <= '9')
	}
	return valid
}
