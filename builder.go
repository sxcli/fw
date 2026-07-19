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
	"errors"
	"fmt"
	"os"
	"sort"

	"sxcli.dev/conf/engine"
	"sxcli.dev/conf/fail"
	"sxcli.dev/fw/internal/registry"
)

// AppBuilder composes an App from the catalog: Accept/AcceptAll admit,
// Order ranks, Alias renames, Build instantiates and validates —
// all-at-once, error returned; the Main terminal (production form)
// arrives with the pipeline re-plumb.
type AppBuilder struct {
	acceptAll bool
	accepts   []string
	order     []string
	renames   []rename
}

type rename struct {
	id    string
	names []string
}

// Builder starts a composition against the catalog.
func Builder() *AppBuilder {
	return &AppBuilder{}
}

// Accept admits services into the composition, by id. Admission is a
// set: repeating an id, or combining Accept with AcceptAll, is
// harmless. An un-accepted catalog entry does not exist for this app.
func (b *AppBuilder) Accept(ids ...string) *AppBuilder {
	b.accepts = append(b.accepts, ids...)
	return b
}

// AcceptAll admits every cataloged service. Order is NOT semantic
// under AcceptAll — ambiguity that ranking would resolve is a
// violation, never an import-order accident.
func (b *AppBuilder) AcceptAll() *AppBuilder {
	b.acceptAll = true
	return b
}

// Order ranks accepted services: ranked beats unranked in
// single-valued matching, slices gather ranked first (in Order
// sequence) then unranked sorted by id, and listings follow the same
// order. Order never admits — ranking an un-accepted id is a
// violation, which doubles as a typo catcher. Multiple calls append;
// ranking an id twice is a violation.
func (b *AppBuilder) Order(ids ...string) *AppBuilder {
	b.order = append(b.order, ids...)
	return b
}

// Alias renames an accepted service for this composition — upstream
// untouched, all operator surfaces follow: Builder.Alias beats the
// registration's aliases entirely (first name is the new primary).
// Beyond collision-fixing this is how a released binary pins its
// operator contract: no upstream rename ever touches a deployed
// config file again.
func (b *AppBuilder) Alias(id string, names ...string) *AppBuilder {
	b.renames = append(b.renames, rename{id: id, names: names})
	return b
}

// Build composes the App: resolve the accept set, apply renames,
// validate the composition (unknown ids, Order and Alias membership,
// alias collisions, the same concrete type twice), instantiate every
// accepted service through its Make (fresh per Build — Apps share
// nothing), and run the value-level checks that needed instances to
// exist (a constructor default outside its own declared domain). All
// violations are joined into one error.
func (b *AppBuilder) Build() (*App, error) {
	return b.buildFrom(defaultRegistry, defaultCollector)
}

// Main is the production terminal: Build, and on violations report
// them all and exit 2 — the standard startup contract; otherwise run
// the App. It never returns.
func (b *AppBuilder) Main() {
	app, err := b.Build()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}
	app.Main()
}

// buildFrom is Build against an explicit catalog and its collector —
// commit violations recorded at registration time surface here, so a
// service that failed its Register never silently "just isn't there".
// Tests use private catalogs.
func (b *AppBuilder) buildFrom(cat *registry.Registry, catalogC *fail.Collector) (*App, error) {
	c := &fail.Collector{}
	if catalogC != nil {
		for _, err := range catalogC.All() {
			c.Add(err)
		}
	}
	accepted := b.admitted(cat, c)
	rank := b.ranked(accepted, c)
	renamed := b.renamed(cat, accepted, c)
	if c.Len() == 0 {
		b.checkAliases(cat, accepted, renamed, c)
		b.checkConcrete(cat, accepted, c)
	}
	var app *App
	if c.Len() == 0 {
		reg := registry.New(c)
		for _, id := range b.composedOrder(accepted, rank) {
			d, _ := cat.ByID(id)
			member := *d // the catalog entry stays pristine; the App owns the copy
			if names, over := renamed[id]; over {
				member.Aliases = names
			}
			_, member.Ranked = rank[id]
			// every catalog entry came through the chain, so Make is
			// always set; instance-carrying entries no longer ride through
			member.Instance, member.ConfigPtr = member.Make()
			reg.Commit(&member)
			defaultsInDomain(&member, c)
		}
		if c.Len() == 0 {
			app = &App{reg: reg}
		}
	}
	var err error
	if c.Len() > 0 {
		err = errors.Join(c.All()...)
		app = nil
	}
	return app, err
}

// admitted resolves the accept set against the catalog, in catalog
// order for AcceptAll plus explicit ids, deduplicated (admission is a
// set).
func (b *AppBuilder) admitted(cat *registry.Registry, c *fail.Collector) map[string]bool {
	out := map[string]bool{}
	if b.acceptAll {
		for _, d := range cat.All() {
			out[d.ID] = true
		}
	}
	for _, id := range b.accepts {
		if _, known := cat.ByID(id); known {
			out[id] = true
		} else {
			c.Fail("accept: unknown service id %q", id)
		}
	}
	return out
}

// ranked validates the Order list — membership required, no repeats —
// and returns each ranked id's position.
func (b *AppBuilder) ranked(accepted map[string]bool, c *fail.Collector) map[string]int {
	out := map[string]int{}
	for i, id := range b.order {
		if !accepted[id] {
			c.Fail("order: %q is not accepted — Order ranks, it never admits", id)
		} else if _, dup := out[id]; dup {
			c.Fail("order: %q ranked twice", id)
		} else {
			out[id] = i
		}
	}
	return out
}

// renamed validates the Alias overrides — membership required, names
// valid — and returns the composed alias sets.
func (b *AppBuilder) renamed(cat *registry.Registry, accepted map[string]bool, c *fail.Collector) map[string][]string {
	out := map[string][]string{}
	for _, r := range b.renames {
		if !accepted[r.id] {
			c.Fail("alias: %q is not accepted", r.id)
		} else if len(r.names) == 0 {
			c.Fail("alias: %q needs at least one name", r.id)
		} else {
			seen := map[string]bool{}
			for _, a := range r.names {
				if !validAlias(a) {
					c.Fail("alias: %q for %q must be lowercase letters, digits and hyphens, starting with a letter", a, r.id)
				} else if a == CoreAlias || a == IntrospectionAlias {
					c.Fail("alias: %q is reserved", a)
				} else if seen[a] {
					c.Fail("alias: %q for %q given twice", a, r.id)
				}
				seen[a] = true
			}
			if _, dup := out[r.id]; dup {
				c.Fail("alias: %q renamed twice", r.id)
			} else {
				out[r.id] = r.names
			}
		}
	}
	return out
}

// checkAliases rejects composed-alias collisions among the accepted,
// naming both claimants.
func (b *AppBuilder) checkAliases(cat *registry.Registry, accepted map[string]bool, renamed map[string][]string, c *fail.Collector) {
	claimed := map[string]string{}
	for _, d := range cat.All() {
		if accepted[d.ID] {
			for _, a := range composedAliases(d, renamed) {
				if prev, taken := claimed[a]; taken {
					c.Fail("alias %q is claimed by both %q and %q — rename one with Builder.Alias", a, prev, d.ID)
				} else {
					claimed[a] = d.ID
				}
			}
		}
	}
}

// checkConcrete rejects the same concrete type accepted twice.
func (b *AppBuilder) checkConcrete(cat *registry.Registry, accepted map[string]bool, c *fail.Collector) {
	byType := map[string]string{}
	for _, d := range cat.All() {
		if accepted[d.ID] {
			key := d.Concrete.String()
			if prev, taken := byType[key]; taken {
				c.Fail("concrete type %s is accepted as both %q and %q", d.Concrete, prev, d.ID)
			} else {
				byType[key] = d.ID
			}
		}
	}
}

// composedOrder returns the accepted ids in composition order: ranked
// first, in Order sequence, then the unranked sorted by id — fully
// deterministic, no import order anywhere.
func (b *AppBuilder) composedOrder(accepted map[string]bool, rank map[string]int) []string {
	var rankedIDs, rest []string
	for id := range accepted {
		if _, isRanked := rank[id]; isRanked {
			rankedIDs = append(rankedIDs, id)
		} else {
			rest = append(rest, id)
		}
	}
	sort.Slice(rankedIDs, func(i, j int) bool { return rank[rankedIDs[i]] < rank[rankedIDs[j]] })
	sort.Strings(rest)
	return append(rankedIDs, rest...)
}

// composedAliases returns a member's operator names after renames.
func composedAliases(d *registry.Descriptor, renamed map[string][]string) []string {
	out := d.Aliases
	if names, over := renamed[d.ID]; over {
		out = names
	}
	return out
}

// defaultsInDomain is the value-level metadata check deferred from the
// registration commit: with the instance born, a constructor default
// outside its own declared Allowed domain is a composition violation.
func defaultsInDomain(d *registry.Descriptor, c *fail.Collector) {
	if meta, has := d.Metadata.(*engine.Meta); has && d.ConfigPtr != nil {
		probes := engine.ProbeFields(d.ConfigPtr)
		for name, fm := range meta.Fields {
			if probe, known := probes[name]; known && len(fm.Allowed) > 0 {
				for _, err := range defaultDomainViolations(d.ID, name, fm.Allowed, probe) {
					c.Add(err)
				}
			}
		}
	}
}
