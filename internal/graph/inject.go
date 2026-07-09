package graph

import (
	"reflect"

	"github.com/sxcli/sxcli-fw/internal/fail"
)

// Inject writes every resolved binding into its owner's inject field.
// Target types were verified during resolution, so Set cannot mismatch;
// the one runtime failure mode is an inject field promoted through a nil
// embedded pointer, which is recorded per field instead of panicking.
// Fields with no targets (unmatched optional dependencies) are left
// untouched, so an optional slice stays nil rather than empty.
func (res Result) Inject(c *fail.Collector) {
	for _, m := range res.Ordered {
		v := reflect.ValueOf(m.Desc.Instance).Elem()
		for _, b := range m.Bindings {
			if len(b.Targets) > 0 {
				if f, err := v.FieldByIndexErr(b.Dep.Index); err == nil {
					if b.Dep.IsSlice {
						s := reflect.MakeSlice(f.Type(), 0, len(b.Targets))
						for _, target := range b.Targets {
							s = reflect.Append(s, reflect.ValueOf(target.Instance))
						}
						f.Set(s)
					} else {
						f.Set(reflect.ValueOf(b.Targets[0].Instance))
					}
				} else {
					c.Fail("service %q field %s: %v", m.Desc.ID, b.Dep.Name, err)
				}
			}
		}
	}
}
