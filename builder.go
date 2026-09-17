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
	"strings"
	"sxcli.dev/rules/solver"

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
	orders    [][]string // every Order call, verbatim — Build judges once-only
	shortPrio [][]string // every ShortArgPriority call, verbatim — same judgment
	renames   []rename
}

type rename struct {
	id   string
	name string
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
// violation, which doubles as a typo catcher. The ranking is declared
// ONCE, atomically: a second call is a violation (the message shows
// both lists), and ranking an id twice within the list is one too.
func (b *AppBuilder) Order(ids ...string) *AppBuilder {
	b.orders = append(b.orders, ids)
	return b
}

// ShortArgPriority declares the composition's one priority list for
// contested short arguments: on a collision within a resolved
// service set, a listed service beats an unlisted one and the
// earlier listing wins among listed; the loser keeps its long form
// only. Core shorts stay reserved regardless. Like Order the list is
// declared once, atomically.
func (b *AppBuilder) ShortArgPriority(ids ...string) *AppBuilder {
	b.shortPrio = append(b.shortPrio, ids)
	return b
}

// Alias renames an accepted service for this composition — upstream
// untouched, all operator surfaces follow: Builder.Alias beats the
// registration's aliases entirely (first name is the new primary).
// Beyond collision-fixing this is how a released binary pins its
// operator contract: no upstream rename ever touches a deployed
// config file again.
func (b *AppBuilder) Alias(id, name string) *AppBuilder {
	b.renames = append(b.renames, rename{id: id, name: name})
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
	// each verb alone is redundant-or-effective and silent; the pair
	// is a contradiction, judged here so init order cannot matter
	if scmDebugEnabled && scmDebugSuppressed {
		c.Fail("FeatureSCMDebug is both enabled and suppressed")
	}
	accepted := b.admitted(cat, c)
	rank := b.ranked(accepted, c)
	shortPriority := b.shortPriority(cat, c)
	renamed := b.renamed(accepted, c)
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
			if name, over := renamed[id]; over {
				member.Alias = name
			}
			_, member.Ranked = rank[id]
			// every catalog entry came through the chain, so Make is
			// always set; instance-carrying entries no longer ride through
			member.Instance, member.ConfigPtr = member.Make()
			reg.Commit(&member)
			defaultsInDomain(&member, c)
		}
		if c.Len() == 0 {
			app = &App{reg: reg, shortPriority: shortPriority}
		}
	}
	var err error
	if c.Len() > 0 {
		err = errors.Join(c.All()...)
		app = nil
	}
	return app, err
}

// admitted resolves the accept set against the catalog, deduplicated
// (admission is a set). The decision itself lives in
// sxcli.dev/rules/solver.Admit: core members are admitted by the
// framework, Accept governs user services only — sxcli-vet's mirror
// makes the same call.
func (b *AppBuilder) admitted(cat *registry.Registry, c *fail.Collector) map[string]bool {
	var members []solver.Member
	for _, d := range cat.All() {
		members = append(members, solver.Member{ID: d.ID, Core: d.Core})
	}
	out, unknown := solver.Admit(members, b.accepts, b.acceptAll)
	for _, id := range unknown {
		c.Fail(solver.AcceptUnknownRule, id)
	}
	return out
}

// shortPriority validates the ShortArgPriority declaration through
// the shared rules — one call, cataloged ids, no repeats — and
// returns the effective list. On a second call the FIRST list stays
// effective, mirroring Order's once-only semantics.
func (b *AppBuilder) shortPriority(cat *registry.Registry, c *fail.Collector) []string {
	if len(b.shortPrio) > 1 {
		c.Fail(solver.ShortPriorityOnceRule, strings.Join(b.shortPrio[0], ", "), strings.Join(b.shortPrio[1], ", "))
	}
	var priority []string
	if len(b.shortPrio) > 0 {
		priority = b.shortPrio[0]
	}
	known := func(id string) bool {
		_, ok := cat.ByID(id)
		return ok
	}
	violations := solver.CheckShortPriority(priority, known)
	for _, v := range violations {
		c.Fail("%s", v.Body)
	}
	return priority
}

// ranked validates the Order declaration — one call, membership
// required, no repeats — and returns each ranked id's position. On a
// second call the FIRST ranking stays the effective one, so later
// verdicts are deterministic while the violation reports.
func (b *AppBuilder) ranked(accepted map[string]bool, c *fail.Collector) map[string]int {
	out := map[string]int{}
	if len(b.orders) > 1 {
		c.Fail(solver.OrderOnceRule, strings.Join(b.orders[0], ", "), strings.Join(b.orders[1], ", "))
	}
	var order []string
	if len(b.orders) > 0 {
		order = b.orders[0]
	}
	for i, id := range order {
		if !accepted[id] {
			c.Fail(solver.OrderNotAcceptedRule, id)
		} else if _, dup := out[id]; dup {
			c.Fail(solver.OrderRankedTwiceRule, id)
		} else {
			out[id] = i
		}
	}
	return out
}

// renamed hands the Alias overrides to the shared rules — the
// verdicts (membership, grammar, reservations, double renames) are
// the solver's; this side only translates and reports.
func (b *AppBuilder) renamed(accepted map[string]bool, c *fail.Collector) map[string]string {
	renames := make([]solver.Rename, len(b.renames))
	for i, r := range b.renames {
		renames[i] = solver.Rename{ID: r.id, Name: r.name}
	}
	out, bodies := solver.CheckRenames(renames,
		func(id string) bool { return accepted[id] },
		[]string{CoreAlias, SystemAlias})
	for _, body := range bodies {
		c.Fail("%s", body)
	}
	return out
}

// checkAliases hands the accepted members' effective names to the
// shared rules for the collision verdict.
func (b *AppBuilder) checkAliases(cat *registry.Registry, accepted map[string]bool, renamed map[string]string, c *fail.Collector) {
	var members []solver.Named
	for _, d := range cat.All() {
		if accepted[d.ID] {
			n := solver.Named{ID: d.ID, Alias: d.Alias}
			if name, over := renamed[d.ID]; over {
				n.Alias = name
				n.Original = d.Alias
			}
			members = append(members, n)
		}
	}
	for _, body := range solver.CheckAliases(members) {
		c.Fail("%s", body)
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
