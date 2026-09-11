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
	"io"
	"os"

	"sxcli.dev/conf/engine"
	"sxcli.dev/conf/fail"
	"sxcli.dev/fw/internal/graph"
	"sxcli.dev/fw/internal/registry"
)

// catalog is the composition's data plane: the registry, the
// operator-name index and the suppressed set — one binary's cataloged
// truth, together with the operations that belong to that data. The
// runtime EMBEDS the live one; the system service receives a snapshot
// copy at attach, so an introspection view holds exactly the data it
// needs and nothing else — no environment, no sources, no seams.
type catalog struct {
	reg           *registry.Registry
	byAlias       map[string]*registry.Descriptor // every operator name → its service; built by index
	suppressed    []string
	shortPriority []string // the composition's contested-short ranking

	// the working set: this invocation's copy of the catalog, built
	// right after dispatch with every applet except the dispatched
	// one ejected — resolution and controls run against a registry
	// holding one applet by construction. The catalog above stays
	// whole; nothing shrinks it again.
	ws        *registry.Registry
	wsByAlias map[string]*registry.Descriptor
}

// index builds the operator-name index: every alias resolves to its
// service. Composed-alias collisions were Build violations — a clash
// here is a framework bug, reported not swallowed.
func (ca *catalog) index(c *fail.Collector) {
	ca.byAlias = map[string]*registry.Descriptor{}
	for _, d := range ca.reg.All() {
		{
			a := d.Alias
			if prev, taken := ca.byAlias[a]; taken && prev != d {
				c.Fail("operator name %q resolves to both %q and %q", a, prev.ID, d.ID)
			} else {
				ca.byAlias[a] = d
			}
		}
	}
}

// composedMembers renders a resolution's resolved service set in
// COMPOSED order
// (Order sequence, then id): the spec promises Order drives listings,
// help sections included — and NewSchema's first-come-first-served
// short forms make section order SEMANTIC, so every schema builder
// MUST come through here. Resolution order is dependency order, not
// listing order; the divergence once handed shorts to the wrong owner
// (review 2026-07-29, D1). The virtual root is never stored, so it
// cannot appear.
func (ca *catalog) composedMembers(res graph.Result) []graph.Member {
	keep := map[string]bool{}
	for _, m := range res.Ordered {
		keep[m.Desc.ID] = true
	}
	var members []graph.Member
	for _, d := range ca.reg.All() {
		if keep[d.ID] {
			members = append(members, graph.Member{Desc: d})
		}
	}
	return members
}

// schema renders the argument schema of one target's resolved
// service set — THE builder: plan, the help fallback and every
// introspection view come through here, so section order (and with
// it short-form
// ownership) has one spelling. The operator surfaces speak the
// target's primary alias, and the sections ride in composed order,
// both by construction.
func (ca *catalog) schema(c *fail.Collector, d *registry.Descriptor, res graph.Result,
	core *engine.Core, ctrl *coreControls, kn *upgradeKnobs) *engine.Schema {
	return engine.NewSchema(c, d.Alias, coreContribs(core, ctrl, kn),
		sections(ca.composedMembers(res)), ca.suppressed, ca.shortPriority)
}

// workingSet builds the invocation's working set for the dispatched
// applet: a catalog snapshot minus every other applet, plus the alias
// index over what remains.
func (ca *catalog) workingSet(dispatched *registry.Descriptor) {
	ca.ws = ca.reg.Snapshot()
	keep := map[string]bool{}
	all := ca.ws.All()
	for i := 0; i < len(all); i++ {
		if !all[i].Applet || all[i].ID == dispatched.ID {
			keep[all[i].ID] = true
		}
	}
	ca.ws.Retain(keep)
	ca.wsByAlias = map[string]*registry.Descriptor{}
	rest := ca.ws.All()
	for i := 0; i < len(rest); i++ {
		ca.wsByAlias[rest[i].Alias] = rest[i]
	}
}

// snapshot returns an independent copy of the catalog's data — the
// world an introspection view answers from. Descriptors are shared
// (data-plane); membership, index and suppressed set are copied, so
// ejection and any later mutation of the live catalog cannot reach
// the copy.
func (ca *catalog) snapshot() *catalog {
	aliases := make(map[string]*registry.Descriptor, len(ca.byAlias))
	for a, d := range ca.byAlias {
		aliases[a] = d
	}
	return &catalog{
		reg:           ca.reg.Snapshot(),
		byAlias:       aliases,
		suppressed:    append([]string(nil), ca.suppressed...),
		shortPriority: append([]string(nil), ca.shortPriority...),
	}
}

// runtime carries every external dependency of one run, injectable for
// hermetic tests. Main builds the production one from the package
// globals and the platform layer.
type runtime struct {
	catalog
	c              *fail.Collector
	argv           []string
	lookupEnv      func(string) (string, bool)
	stdout         io.Writer
	stderr         io.Writer
	locations      func(appletID string) []engine.Location
	stat           func(string) (int64, error)
	lstat          func(string) error
	open           func(string) (io.ReadCloser, error)
	openPinned     func(string) (io.ReadCloser, error)
	configMaxBytes uint64           // effective config file size cap in bytes; 0 = unlimited
	execApplet     func(Applet) int // nil → applet.Run(); the SCM handler overrides
	superuser      func() bool      // platform truth: id 0 on unix, elevated token on Windows
	reported       bool
	translatorID   string // id of the sole Translator-providing service, "" = none
}

func productionRuntime(app *App, argv []string, execApplet func(Applet) int) *runtime {
	return &runtime{
		catalog:   catalog{reg: app.reg, suppressed: effectiveSuppressedCore(), shortPriority: app.shortPriority},
		c:         &fail.Collector{},
		argv:      argv,
		lookupEnv: os.LookupEnv,
		stdout:    os.Stdout,
		stderr:    os.Stderr,
		locations: engine.ProductionLocations,
		stat:      engine.StatRegular,
		superuser: isSuperuser,
		lstat: func(path string) error {
			_, err := os.Lstat(path)
			return err
		},
		open:           func(path string) (io.ReadCloser, error) { return os.Open(path) },
		openPinned:     engine.OpenPinned,
		configMaxBytes: configMaxBytes,
		execApplet:     execApplet,
	}
}
