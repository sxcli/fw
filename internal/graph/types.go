// Package graph resolves the service composition of one invocation: the
// dependency closure of the dispatched applet, the resolved injection
// targets of every member, and a dependency-ordered start sequence. Like
// the registry it is ignorant of the framework's interfaces: the applet
// and always-on services arrive as plain ids computed by the root
// package.
package graph

import (
	"github.com/sxcli/sxcli-fw/internal/fail"
	"github.com/sxcli/sxcli-fw/internal/registry"
)

// Controls is the config-driven service control surface (the core's
// disable/enable/override settings). Disable removes services from the
// closure even when required; Enable forces services (and their
// transitive dependencies) in; Override remaps ids named in inject tags.
// Every id in Disable and Enable, and every Override substitute, must be
// registered. An Override key is just a name — it may refer to an
// unregistered id so a missing implementation can be substituted, and a
// generic config may carry overrides irrelevant to this applet — so an
// override matching no dependency is not an error; it is reported via
// Result.UnusedOverrides for the caller to log.
type Controls struct {
	Disable  []string
	Enable   []string
	Override map[string]string // requested id → substitute id
}

// Binding is one resolved inject field of a closure member.
type Binding struct {
	Dep     registry.DepField
	Targets []*registry.Descriptor // registration order; empty only for unmatched optional fields
}

// Member is one closure member with its resolved bindings.
type Member struct {
	Desc     *registry.Descriptor
	Bindings []Binding
}

// Result is the resolved composition of one invocation.
type Result struct {
	// Ordered is the closure in dependency order: dependencies before
	// dependents (the Start order; Stop is the exact reverse). Within a
	// dependency cycle the order degrades to registration order.
	Ordered []Member
	// Cycles lists every dependency cycle as service ids in
	// registration order. Cycles are legal but weaken the Start
	// promise; the caller should log a warning per entry.
	Cycles [][]string
	// UnusedOverrides lists override keys that remapped nothing,
	// sorted. Legal (generic configs, unlinked rescue targets) but
	// worth a warning: a typo here silently changes nothing.
	UnusedOverrides []string
}

// resolver carries the working state of one Resolve call.
type resolver struct {
	reg          *registry.Registry
	c            *fail.Collector
	disabled     map[string]bool
	override     map[string]string
	overrideUsed map[string]bool
	closure      map[string]bool
	result       Result
}
