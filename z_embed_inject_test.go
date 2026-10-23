package fw

import (
	"strings"
	"testing"
)

type embedStore interface{ Probe() string }

// EmbedDeps carries an inject tag; embedding it is not how a service
// declares a dependency (spec §4), exported or not.
type EmbedDeps struct {
	V embedStore `inject:""`
}

type embedCarrier struct {
	EmbedDeps
	N int
}

func (e *embedCarrier) Configured() error { return nil }
func (e *embedCarrier) Run() int          { return 0 }

type embedHidden struct {
	H embedStore `inject:""`
}

type hiddenCarrier struct {
	*embedHidden
	N int
}

func (h *hiddenCarrier) Configured() error { return nil }
func (h *hiddenCarrier) Run() int          { return 0 }

// plainHelper has no inject tags: embedding it is ordinary Go.
type plainHelper struct{ count int }

type helperCarrier struct {
	plainHelper
	Dep embedStore `inject:""`
}

func (h *helperCarrier) Configured() error { return nil }
func (h *helperCarrier) Run() int          { return 0 }

func TestInjectThroughEmbeddedStructIsViolation(t *testing.T) {
	reg, c := catalogWorld()
	NewBareRegistration("embed/carrier", func() *embedCarrier { return &embedCarrier{} }).
		Alias("carrier").registerInto(reg, c)
	NewBareRegistration("embed/hidden", func() *hiddenCarrier { return &hiddenCarrier{} }).
		Alias("hidden").registerInto(reg, c)
	all := ""
	for _, err := range c.All() {
		all += err.Error() + "\n"
	}
	for _, want := range []string{
		`service "embed/carrier" field V: field V is injected through the embedded struct EmbedDeps — declare the dependency on the service itself`,
		`service "embed/hidden" field H: field H is injected through the embedded struct embedHidden — declare the dependency on the service itself`,
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in:\n%s", want, all)
		}
	}
	for _, id := range []string{"embed/carrier", "embed/hidden"} {
		if d, registered := reg.ByID(id); registered && len(d.Deps) != 0 {
			t.Errorf("%s: a dependency reached through embedding must never be collected: %+v", id, d.Deps)
		}
	}
}

func TestEmbeddedHelperWithoutTagsIsFine(t *testing.T) {
	reg, c := catalogWorld()
	NewBareRegistration("embed/helper", func() *helperCarrier { return &helperCarrier{} }).
		Alias("helper").registerInto(reg, c)
	if c.Len() != 0 {
		t.Fatalf("a tagless embedded helper is ordinary Go: %v", c.All())
	}
	d, _ := reg.ByID("embed/helper")
	if len(d.Deps) != 1 || d.Deps[0].Name != "Dep" {
		t.Errorf("the service's own dependency must be collected: %+v", d.Deps)
	}
}
