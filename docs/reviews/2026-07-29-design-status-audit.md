# Design-doc status audit — 2026-07-29

## Verdict

13 docs audited (8 fw, 5 conf); all suites green (fw/conf/rules/vet/completion); 5 docs implemented as claimed, 6 cleanly unimplemented with no contradicting code drift, 2 STATUS blocks overclaim on their spec/vet touch items — and the spec's Introspection section is flatly contradicted by the shipped system service.

Baseline: `go test ./...` green in sxcli-fw, sxcli-conf, sxcli-rules, sxcli-vet, sxcli-completion (2026-07-29). Note: the task list named "service-id-slash-floor" and "alias treatment unification"; the former is `v0.3.0-to-v0.4.0-disjoint-identities.md`, the latter has **no standalone doc** in either design dir — its content is split across single-home-checks (rule 4, alias grammar single home — implemented) and disjoint-identities (alias/id disjointness — implemented).

## fw: v0.3.0-to-v0.4.0-applets-listing.md

Intent: `--applets` core argument lists the binary's public applets pre-dispatch; the listing leaves help/usage. No STATUS block.

| Claim | Status | Evidence |
|---|---|---|
| `--help` stops listing applets | MOOT (already true) | fw/main.go:788-800 — `help()` renders schema only; the listing has only ever lived in dispatch-failure `usage()` (spec line 1656 says exactly this) |
| New `--applets` core argument, exits 0 | NOT IMPLEMENTED | no `applets` conf tag in fw/main.go:242-261; no `FeatureApplets` in fw/suppress.go:17-48 |
| Pre-dispatch recognition in `run()` | NOT IMPLEMENTED | fw/main.go dispatch path has no such short-circuit |
| `usage()` drops the applet list, points at `--applets` | NOT IMPLEMENTED | fw/main.go:203-211 still prints `applets:` + list |
| Reads the catalog (immutable Build result) | NOT IMPLEMENTED (blocked) | depends on the catalog/working-set split (one-applet-per-closure doc), itself unimplemented |
| completion completes `--applets` | NOT IMPLEMENTED | no hits in sxcli-completion |

Drift: none. The spec already brackets this as future (spec lines 613, 1656) — code, spec and doc agree on "not yet".

## fw: v0.3.0-to-v0.4.0-core-arguments-argv-only.md

Intent: no core argument is ever readable from the environment; enforced structurally. STATUS (2026-07-28): "IMPLEMENTED except the tiers follow-up."

STATUS accuracy: **mostly accurate; slightly overclaiming.** Everything the STATUS *enumerates* landed. But the header "IMPLEMENTED except the tiers follow-up" is wider than the enumeration — two touch-list items did not land: the spec §6 argv-only security statement (no `argv-only`/"environment must never" text in docs/superpowers/specs/2026-07-09-sxcli-fw-design.md; only the CHANGELOG carries it, fw/CHANGELOG.md:5) and the sxcli-vet ledger row/static mirror (vet applies `tags.Check(tree, tags.Section)` only — sxcli-vet/internal/vet/tags.go:38; no ContribEnv reference anywhere in vet code or docs).

| Claim | Status | Evidence |
|---|---|---|
| `env:"-"` on Disable/Enable/Override | IMPLEMENTED | fw/main.go:242-244 |
| `env:"-"` on conf Core (Config/WriteConfig/Help/ValidateConfig) | IMPLEMENTED | sxcli-conf/engine/types.go:87-90 |
| `env:"-"` on upgrade knobs | IMPLEMENTED | fw/main.go:260-261 |
| Structural refusal — contribution field without `env:"-"` is a violation | IMPLEMENTED | sxcli-rules/tags/check.go:56 (`ContribEnvRule`), :163-167 (Contribution mode); wired at sxcli-conf/engine/schema.go:161; tested rules/tags/z_check_test.go:313 |
| Injection pin tests (env-set control not honored) | IMPLEMENTED | fw/z_main_test.go:444-446 (`BIN__DISABLE` must not eject) |
| Completion env-blindness (introspection view env-blind by construction) | IMPLEMENTED | fw/systemservice.go:36-43 (`envless.lookupEnv` stub) — matches STATUS's "landed better than specified" |
| Controls keep the config-file door | IMPLEMENTED | commit 8ef7e72 ("dump carries the core's file-settable controls"); no `dump:"-"` on main.go:242-244 |
| spec §6 states argv-only as security invariant | NOT IMPLEMENTED | no such statement in the spec; CHANGELOG only (fw/CHANGELOG.md:5) |
| sxcli-vet check: core-tier field without `env:"-"` | NOT IMPLEMENTED | vet uses `tags.Section` mode only (vet/tags.go:38); no Contribution-mode call, no ledger row |

Drift: none.

## fw: v0.3.0-to-v0.4.0-disjoint-identities.md

Intent: service-id grammar requires ≥1 `/`; id and alias grammars disjoint by construction. STATUS (2026-07-26): "IMPLEMENTED except the tie cleanup."

STATUS accuracy: **accurate**, including its own correction (single home is `rules/grammar.ValidServiceID`, not `internal/registry`).

| Claim | Status | Evidence |
|---|---|---|
| `/` floor in the id grammar | IMPLEMENTED | sxcli-rules/grammar/grammar.go:35-59 ("AT LEAST ONE '/'"; :46 "a single segment never validates") |
| fw delegates to the single home | IMPLEMENTED | fw/identity.go:44 (`validServiceID` → `grammar.ValidServiceID`) |
| vet consumes the same function | IMPLEMENTED | sxcli-vet/internal/vet/tags.go:53 |
| Aliases never contain `/` → grammars disjoint | IMPLEMENTED | grammar.go:78-80 (`ValidAlias` = hyphenName, no `/`) |
| Fixtures/docs path-shaped | IMPLEMENTED | e.g. fw/internal/graph/z_graph_test.go:218-368 (`t/workerb`, `t/app`) |
| Spec states the rule | IMPLEMENTED | spec lines 360-361 ("**at least one `/`**") |
| CHANGELOG entry | IMPLEMENTED | fw/CHANGELOG.md:13 |
| `resolveRef`/tie removal | NOT IMPLEMENTED (as STATUS says — deferred to explicit-control-vocabulary) | fw/main.go:98-121 (triple return, `tied`), :693-717 (callers) |

Drift: none.

## fw: v0.3.0-to-v0.4.0-explicit-control-vocabulary.md

Intent: `--enable-id`/`--enable-alias`/`--disable-id`/`--disable-alias` replace `--enable`/`--disable`; `--override` id-only; `resolveRef` deleted; applets not addressable. No STATUS block.

| Claim | Status | Evidence |
|---|---|---|
| Four new flags exist | NOT IMPLEMENTED | zero hits for `enable-id`/`disable-alias`/etc. across fw and completion |
| Plain `--enable`/`--disable` removed | NOT IMPLEMENTED | fw/main.go:242-244 still declares them; fw/suppress.go:28-33 still names FeatureDisable/FeatureEnable/FeatureOverride |
| `--override` id-only both sides | NOT IMPLEMENTED | fw/main.go:712-717 resolves both sides through `resolveRef` (aliases accepted) |
| `resolveRef` removed | NOT IMPLEMENTED | fw/main.go:98-121 intact |
| "applet %q is disabled" error removed | NOT IMPLEMENTED | fw/main.go:337 intact |
| Applet references report "unknown service" | NOT IMPLEMENTED | controls resolve against the full registry (applets included) |
| completion HintServiceID splits per flag | NOT IMPLEMENTED | sxcli-completion/engine/engine.go:193 — single `fw.HintServiceID` pool |

Drift: none contradicting. One stale internal citation: the Why section cites "`graph.go` `mapped`" — the override remap now lives in sxcli-rules/solver/closure.go:190 (moved by shared-rules Tier 3); the mechanics the doc relies on (every requested id passes the remap) are unchanged, so the argument holds, but the file reference is dead. Prerequisite (disjoint grammars) is in place; `solver.Controls.Override` is already `map[string]string` from→to over ids (solver/model.go:54) — the graph side needs no vocabulary change.

## fw: v0.3.0-to-v0.4.0-id0-start-gate.md

Intent: refuse startup at euid 0 unless the applet declares `.AllowsID0()`. No STATUS block; two Open items undecided.

| Claim | Status | Evidence |
|---|---|---|
| `.AllowsID0()` chain method + Descriptor field | NOT IMPLEMENTED | zero hits for `AllowsID0` in fw |
| euid gate in main.go | NOT IMPLEMENTED | zero hits for `Geteuid`/`geteuid` in fw |
| Testable platform seam | NOT IMPLEMENTED | fw/runtime.go has no geteuid field |

Drift: none; nothing in the code preempts either Open item (serving-surface scope, refuse-vs-warn). Note the field-source-tiers doc's Open item "when the restriction bites" cross-references this gate — deciding one constrains the other.

## fw: v0.3.0-to-v0.4.0-one-applet-per-closure.md

Intent: one-applet-per-invocation becomes a checked graph invariant via the catalog/working-set split, applet eject before resolution, per-door checks and a backstop. No STATUS block.

| Claim | Status | Evidence |
|---|---|---|
| Controls cannot address applets | NOT IMPLEMENTED | only guard is the dispatched-applet disable check, fw/main.go:333-337; `--enable <dormant-applet>` passes unexamined |
| Applet eject before resolution | NOT IMPLEMENTED | `graph.Resolve` at fw/main.go:340 runs against the full registry; `Retain` runs after, main.go:376-385 |
| Catalog/working-set split | NOT IMPLEMENTED | `Registry.Retain` mutates the live registry in place, fw/internal/registry/registry.go:101-110; no per-invocation copy |
| Skip-ejection-for-introspectors branch removed | NOT IMPLEMENTED — and the branch MOVED | main.go:381-385: the special case survives, re-keyed from IntrospectionID to `!keep[system.ID]` by the system-service work; the doc's anchor "main.go:383" happens to still be the right line |
| `Descriptor.Applet bool` | NOT IMPLEMENTED (fw) / PARTIALLY (ecosystem) | fw/internal/registry/types.go has `Core` (:43) but no `Applet`; however solver already reserves `Member.Applet` "for the one-applet verdicts" (sxcli-rules/solver/model.go:32) and vet records `RegFact.Applet` and solves per applet-root (sxcli-vet/internal/vet/registrations.go:47, :391, :279-307) |
| Dormant applets invisible to dependency resolution | NOT IMPLEMENTED | solver candidate pools ignore `Member.Applet` (no non-test use in solver/closure.go, match.go, bind.go); enables seeded without an applet test (solver/closure.go:53, :97) |
| Post-resolution backstop (exactly one Applet) | NOT IMPLEMENTED | no such check after Resolve in main.go |
| Spec carries the rule verbatim + doors | NOT IMPLEMENTED | spec still promises "A closure containing the Introspector is never ejected" (spec ~line 481) — the opposite of unconditional ejection |

Drift: the only movement since ratification is TOWARD the design (Applet plumbing in solver and vet, landed with shared-rules Tier 3), not against it. The live bug the doc documents (`--enable <applet>` silently admits a dormant applet's config/lifecycle surface) is still live and still silent — no half-implementation leaks a new surface, but the old hole is unchanged.

## fw: v0.3.0-to-v0.4.0-shared-rules-module.md

Intent: `sxcli.dev/rules` — one implementation for everything fw and vet both validate. STATUS (2026-07-26): "largely IMPLEMENTED", corrections inline.

STATUS accuracy: **accurate**, including the release caveat.

| Claim | Status | Evidence |
|---|---|---|
| Packages grammar/tags/migration/solver | IMPLEMENTED | sxcli-rules/{grammar,tags,migration,solver} |
| Tier 1 complete (incl. inject-tag, env derivation) | IMPLEMENTED | grammar.go:159 `ParseInjectTag`, :190 `EnvSegment`, :220 `DeriveEnv`, :104 `HasSepRun`, :123/:132 `ValidLong`/`ValidShort` |
| Tier 2 adapters (conf reflect, vet go/types) | IMPLEMENTED | sxcli-conf/engine/fieldtree.go:20; sxcli-vet/internal/vet/fieldtree.go |
| Tier 3 solver, fw graph as reflect adapter, vet per-root | IMPLEMENTED | fw/internal/graph/graph.go:57 consumes solver verdicts; vet registrations.go:289-307 |
| Module local-only, unreleased | ACCURATE | `v0.0.0` + `replace` in fw/conf/vet/completion go.mod; sxcli-rules has no git tags |
| Release order rules→conf→fw→vet | NOT IMPLEMENTED (still the plan) | conf tags stop at v0.1.1, fw at v0.3.0, rules untagged |
| Tier 4 structured-violations concern | OPEN (as recorded) | message bodies remain strings next to rules |

Drift: none.

## fw: v0.3.0-to-v0.4.0-the-system-service.md

Intent: the core's facilities become a cataloged member (`fw/system` vocabulary, init-registered facade, framework admission). STATUS (2026-07-28): "IMPLEMENTED (Y1-Y5 …)".

STATUS accuracy: **accurate for code; overclaims by omission on the spec.** The touch list includes "spec §4/§5 — the core-services-are-services statement, the admission rule, the facade contract"; none of it landed, and the spec's existing Introspection section now **contradicts the code**.

| Claim | Status | Evidence |
|---|---|---|
| `fw/system` declarations package | IMPLEMENTED | fw/system/system.go; `System` is an interface (:96), per the STATUS refinement |
| `fw.init()` registers the member | IMPLEMENTED | fw/systemservice.go:51-55 (`NewBareRegistration(system.ID, …).Alias(SystemAlias).…core()`) |
| Auto-admission (`Descriptor.Core`, unforgeable `core()`) | IMPLEMENTED | fw/internal/registry/types.go:43; fw/catalog.go:100; alias-squat guards fw/builder.go:209, fw/catalog.go:192 |
| Hand-registration + squat-check + IntrospectionID retired | IMPLEMENTED | zero hits for `IntrospectionID`/`IntrospectionAlias`; commit dc6928c |
| Env-blind introspection view | IMPLEMENTED | fw/systemservice.go:36-43 |
| completion migrates to system vocabulary | IMPLEMENTED | sxcli-completion/bash/types.go:41, zsh/types.go:43 (`Sys system.System \`inject:""\``); commit 17d1fc1 |
| vet surgical skip (returned-chain rule), ledger #22 | IMPLEMENTED | vet commit f5cd3d6 "fw root analyzed, its machinery exempted surgically"; vet suite green on the real tree |
| spec §4/§5 statements | NOT IMPLEMENTED — spec CONTRADICTED-BY-CODE | spec lines ~468-483 still describe: reserved id `introspection` + squat-check (retired), injection "by concrete type (`*fw.Introspector`)" (retired), "a closure containing the Introspector is never ejected" (now keyed on `system.ID`, main.go:381-385) |

Additional stale text: sxcli-completion/engine/types.go:21-24 — `Source` doc comment still says "the narrow view of the core's `*fw.Introspector` … `*fw.Introspector` satisfies it implicitly"; the concrete Introspector is no longer the injected artifact. `engine.Source` survived (permitted — "may shrink … or die"), but the comment describes a retired wiring.

## conf: v0.1.0-to-v0.1.1-file-schema.md

Intent: `--upgrade-config` builds a file schema (`NewFileSchema`) free of command-line checks. Written post-hoc ("What we changed"); no STATUS block needed.

| Claim | Status | Evidence |
|---|---|---|
| `NewFileSchema` exists, shares validators | IMPLEMENTED | sxcli-conf/engine/schema.go:140-161 (NewSchema builds on it, :91) |
| conf front door uses it | IMPLEMENTED | sxcli-conf/conf.go:327 |
| fw `upgradeConfig` uses it | IMPLEMENTED | fw/main.go:468 |
| Open: corpus fixture pair pinning the constructor difference | NOT IMPLEMENTED | no such fixture pair found; the untracked fw/testdata/confbox/main.go is a conf smoke program, not it |

Drift: none.

## conf: v0.1.1-to-v0.2.0-field-source-tiers.md

Intent: restrict which file-location tier may set a field (controls → system tier only). No STATUS block; four Open items undecided.

| Claim | Status | Evidence |
|---|---|---|
| `Location.Tier` + constructor stamping | NOT IMPLEMENTED | zero `Tier` hits in conf engine (only `TestTierSuppressionPrunesTheSearch`, sxcli-conf/z_conf_test.go:197 — location *suppression*, a different mechanism) |
| `RestrictSource` chain step | NOT IMPLEMENTED | zero hits in conf and fw |
| File application enforces allowlists | NOT IMPLEMENTED | applyObject/applyVersioned have no tier logic |
| fw wires controls to TierSystem | NOT IMPLEMENTED | fw controls are settable from any file location today |

Drift: none contradicting. Note this doc formally owns the "controls' config-file door" question that the argv-only STATUS delegates to it — the delegation is consistent in both directions. The location-tier *vocabulary* already exists as suppress features (FeatureSystemConfig/UserConfig/CompanionConfig), which the design can hang tiers on.

## conf: v0.1.1-to-v0.2.0-positional-interleaving.md

Intent: bare tokens interleave with flags while indexed `pos:"N"` slots remain; tail-only discipline narrows to `pos:"rest"`. No STATUS block.

| Claim | Status | Evidence |
|---|---|---|
| Slot-aware interleaving in the parser | NOT IMPLEMENTED | sxcli-conf/engine/args.go:72 — blanket refusal "unexpected arguments %q before %s: positionals must come last" intact |
| Rest-scoped refusal | NOT IMPLEMENTED | same |
| completion: silence begins at rest | NOT IMPLEMENTED | completion still carries the blanket bare-token silence from batch c9ecf9e |
| spec §6 narrowed rule | NOT IMPLEMENTED | not present |

Drift: none — parser and completion agree on the OLD rule, so parity is intact; implementing the parser change without the completion change would break the parity this repo family treats as mandatory (the doc already schedules both).

## conf: v0.1.1-to-v0.2.0-presence-migrations.md

Intent: presence-aware migration chains; minimal dumps; the out-parameter converter. Amended 2026-07-28 with inline RATIFIED/REALIZED markers (a de-facto STATUS).

STATUS accuracy: **accurate.**

| Claim | Status | Evidence |
|---|---|---|
| `NewStep[From,To](from, fn(old, has, out, set))` out-parameter shape | IMPLEMENTED | sxcli-conf/engine/migrate.go:51; front door `conf.Presence` alias conf.go:73 |
| `Presence` type / presence.go | IMPLEMENTED | sxcli-conf/engine/presence.go:43 |
| Acceptance-example and absence-preservation pins | IMPLEMENTED | engine/z_upgrade_test.go:220 `TestUpgradeAcceptanceExample`; engine/z_migrate_test.go:105 `TestMigrationPreservesAbsence` |
| `warnMaterialized` / `emptyValue` retired | IMPLEMENTED | zero hits in conf |
| Chain json-name mandate, shared + vet | IMPLEMENTED | sxcli-rules/tags/check.go:316 `ChainJSON`; engine/migrate.go:27-28 consumes rules; vet commits 4206a2b, e0a0dc8 (four-parameter Step mirror) |
| fw `Step` mirror unchanged in shape | IMPLEMENTED | fw/catalog.go:126 delegates to `engine.NewStep` |
| Breaking `conf.Step` ships as v0.2.0 | NOT YET RELEASED | sxcli-conf git tags stop at v0.1.1 — consistent with the shared-rules release-train note |

Drift: none.

## conf: v0.1.1-to-v0.2.0-single-home-checks.md

Intent: every check gets exactly one implementation. STATUS (2026-07-26): "DONE, with the homes one level higher than planned."

STATUS accuracy: **accurate** (it corrects its own pre-rules-module homes).

| Claim | Status | Evidence |
|---|---|---|
| Chain shape single home | IMPLEMENTED | sxcli-rules/migration/migration.go:53 `Check`; engine `ChainShape` migrate.go:65-75 |
| Version finder | IMPLEMENTED | sxcli-rules/tags/check.go:59-63 `VersionField`, consumed at :302 |
| Sep-run exported, fw inline copy gone | IMPLEMENTED | grammar.go:97-104 `SepRunRule`/`HasSepRun`; fw/identity.go:57 delegates alias wholesale |
| Id grammar single home | IMPLEMENTED | grammar.go:45; fw/identity.go:44; vet tags.go:53 |
| Body/prefix message ownership | IMPLEMENTED | e.g. `SepRunRule` body in grammar; callers wrap (per engine/schema.go:421 comment) |

Drift: none.

## Cross-doc conflicts

1. **Spec vs code (system service)** — the only outright contradiction found. docs/superpowers/specs/2026-07-09-sxcli-fw-design.md (Introspection section, ~468-483) describes the retired mechanism (reserved `introspection` id, squat-check, concrete `*fw.Introspector` injection). The system-service doc's own touch list owed this update; its STATUS says IMPLEMENTED without it.
2. **one-applet-per-closure vs the shipped system service.** The design mandates unconditional ejection and catalog-fed introspection; the shipped facade re-anchored the never-eject special case to `keep[system.ID]` (fw/main.go:381-385) and its Introspector reads the live registry, not a catalog. Not a code-vs-ratified-design contradiction today (the design is unimplemented), but implementing one-applet now requires re-deciding the system member's ejection story — the two docs' mechanisms must land as one design conversation.
3. **explicit-control-vocabulary ↔ one-applet-per-closure duplicate normative text.** Claim 4 of the former and door 1 of the latter state the same rule in near-identical words, each deferring enforcement to the other ("closure-side enforcement lives in…", "control-surface face of…"). Acceptable as cross-references, but they must land together or the rule exists in neither.
4. **Ordering chain (implicit, consistent):** applets-listing depends on the catalog/working-set split (one-applet); explicit-control-vocabulary depends on disjoint-identities (done) and finishes its tie cleanup; field-source-tiers supersedes argv-only's "file door for now" note (both docs say so — consistent); field-source-tiers' "when the restriction bites" Open item is entangled with id0-start-gate.
5. **Stale references, no semantic conflict:** explicit-control-vocabulary cites `graph.go mapped` (now sxcli-rules/solver/closure.go:190); one-applet cites "main.go:383 IntrospectionID special case" (now `system.ID`, same lines); completion engine/types.go:21-24 comment still names `*fw.Introspector`.

## Recommended next actions

1. Fix the stale comment at sxcli-completion/engine/types.go:21-24 (`*fw.Introspector` → `system.Introspector`). One-line.
2. Update the spec's Introspection section to the shipped system-service model (and add the promised admission-rule statement) — closes the only code-contradicting document.
3. Add the argv-only security invariant to spec §6 (core arguments), completing that doc's touch list; then its STATUS header is fully earned.
4. Amend argv-only STATUS (or land the vet Contribution-mode check) so the vet static-mirror row is either done or explicitly deferred.
5. Add the file-schema doc's open corpus fixture pair (NewSchema vs NewFileSchema on the same input).
6. Refresh stale citations in explicit-control-vocabulary (`graph.go mapped` → solver/closure.go) and one-applet (`IntrospectionID` → `system.ID`) so implementers land on the right code.
7. Implement explicit-control-vocabulary (smallest of the unimplemented v0.4.0 designs; disjoint-identities prerequisite done; retires resolveRef/tie, closing disjoint-identities' last item).
8. Implement one-applet-per-closure together with the system-member ejection re-decision (conflict 2); solver `Member.Applet` and vet's per-root plumbing already await it.
9. Implement applets-listing (after 8 — needs the catalog).
10. Decide id0-start-gate's two Open items jointly with field-source-tiers' "when the restriction bites", then implement each.
11. Implement positional-interleaving (parser + completion in one change, for parity).
12. Run the release train (rules v0.1.0 → conf v0.2.0 → fw v0.4.0 → vet, completion) once the above lands.
