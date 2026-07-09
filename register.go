package sxclifw

import (
	"fmt"
	"reflect"

	"github.com/sxcli/sxcli-fw/internal/config"
	"github.com/sxcli/sxcli-fw/internal/fail"
	"github.com/sxcli/sxcli-fw/internal/registry"
)

// defaultCollector accumulates every startup violation across all
// phases; Main reports its content and exits when it is non-empty.
var defaultCollector = &fail.Collector{}

// defaultRegistry is populated by Register calls from package init()
// functions; Main validates and consumes it.
var defaultRegistry = registry.New(defaultCollector, checkReservedID, checkAppletLifecycle, config.ValidateConfig)

type registerOptions struct {
	interfaces []reflect.Type
	config     any
}

// RegisterOption configures a single Register call.
type RegisterOption func(*registerOptions)

// Provides declares an interface the registered instance provides. Only
// declared interfaces participate in dependency injection — a service is
// never injected somewhere just because it accidentally satisfies an
// interface. Declaring an interface the instance does not implement is a
// registration error. Always-on status is declared the same way, with
// Provides[AlwaysOn]().
func Provides[I any]() RegisterOption {
	t := reflect.TypeOf((*I)(nil)).Elem()
	return func(o *registerOptions) {
		o.interfaces = append(o.interfaces, t)
	}
}

// WithConfig attaches the service's Configuration struct. cfgPtr must be
// a non-nil pointer to struct; its field values at registration are the
// defaults. The framework fills the same struct in place with the merged
// configuration before Configured is called — there is never a second
// config instance.
func WithConfig(cfgPtr any) RegisterOption {
	return func(o *registerOptions) {
		o.config = cfgPtr
	}
}

// Register registers a service instance under a unique id. It is meant
// to be called from package init() functions; one package may register
// many services. The id must be a non-empty, all-lowercase go identifier
// and unique within the binary; the same concrete struct type may be
// registered only once. Register never panics — every violation is
// recorded and reported at startup, all problems at once.
//
// After Register the instance belongs to the framework: register a
// literal (Register("x", &X{}, ...)) and do not keep references to it.
// Services outside the resolved closure are ejected from the registry so
// their instances can be garbage collected — a kept package-level
// reference only defeats that reclamation.
func Register(id string, instance any, opts ...RegisterOption) {
	var o registerOptions
	for _, opt := range opts {
		opt(&o)
	}
	defaultRegistry.Register(id, instance, registry.Options{Interfaces: o.interfaces, Config: o.config})
}

// reservedCoreID is the service id under which the framework core's own
// configuration lives; no service may claim it.
const reservedCoreID = "core"

func checkReservedID(d *registry.Descriptor) error {
	var err error
	if d.ID == reservedCoreID {
		err = fmt.Errorf("service id %q is reserved for the framework core", d.ID)
	}
	return err
}

func checkAppletLifecycle(d *registry.Descriptor) error {
	var err error
	if _, applet := d.Instance.(Applet); applet {
		if _, hasLifecycle := d.Instance.(Stopper); hasLifecycle {
			err = fmt.Errorf("service %q: an applet must not implement Starter or Stopper", d.ID)
		}
	}
	return err
}
