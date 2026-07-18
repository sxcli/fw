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

package sxclifw

import (
	"fmt"
	"reflect"

	"sxcli.dev/fw/internal/config"
	"sxcli.dev/fw/internal/fail"
	"sxcli.dev/fw/internal/registry"
)

// defaultCollector accumulates every startup violation across all
// phases; Main reports its content and exits when it is non-empty.
var defaultCollector = &fail.Collector{}

// defaultRegistry is populated by Register calls from package init()
// functions; Main validates and consumes it.
var defaultRegistry = registry.New(defaultCollector, checkReservedID, checkAppletLifecycle, checkVisibility, config.ValidateConfig, checkMetadata)

type registerOptions struct {
	interfaces []reflect.Type
	config     any
	metadata   *Metadata
	hidden     bool
	system     bool
}

// RegisterOption configures a single Register call.
type RegisterOption func(*registerOptions)

// Provides declares an interface the registered instance provides. Only
// declared interfaces participate in dependency injection — a service is
// never injected somewhere just because it accidentally satisfies an
// interface. Declaring an interface the instance does not implement is a
// registration error.
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

// Hidden marks a registered applet as a hidden command: it stays
// selectable by an explicit first-token selector, but is absent from
// usage listings and is never matched by basename/symlink dispatch.
// Visibility is registration-time policy, not a capability, hence an
// option rather than an interface. Valid only on applets.
func Hidden() RegisterOption {
	return func(o *registerOptions) {
		o.hidden = true
	}
}

// System marks a registered applet as machinery of the binary — an
// entry point invoked by tooling (a shell completion script, a
// diagnostic harness), never typed by a human. System implies Hidden
// and additionally excludes the applet from single-applet counting, so
// registering one never flips an existing binary's dispatch mode; its
// id remains selectable as a first token in every mode, including
// single-applet mode. Valid only on applets.
func System() RegisterOption {
	return func(o *registerOptions) {
		o.system = true
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
	defaultRegistry.Register(id, instance, registry.Options{Interfaces: o.interfaces, Config: o.config, Metadata: o.metadata, Hidden: o.hidden || o.system, System: o.system})
}

// CoreAlias is the operator-facing name of the framework core — its
// config section, the virtual root of every resolution, and the
// synthesized introspection entry; no service may claim it.
const CoreAlias = config.CoreID

func checkReservedID(d *registry.Descriptor) error {
	var err error
	if d.ID == CoreAlias {
		err = fmt.Errorf("service id %q is reserved for the framework core", d.ID)
	} else if d.ID == IntrospectionAlias && d.Concrete != reflect.TypeOf(&Introspector{}) {
		err = fmt.Errorf("service id %q is reserved for the core's Introspector", d.ID)
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

// checkVisibility rejects the Hidden/System applet-visibility options
// on services that are not applets — visibility is dispatch policy and
// only applets are dispatched.
func checkVisibility(d *registry.Descriptor) error {
	var err error
	if d.Hidden || d.System {
		if _, applet := d.Instance.(Applet); !applet {
			err = fmt.Errorf("service %q: Hidden/System apply only to applets", d.ID)
		}
	}
	return err
}
