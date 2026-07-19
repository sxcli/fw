# Composition rewrite — file inventory

Branch `composition`. The anti-miss instrument: every file classified,
every global sentenced, every test ledgered. Spec: design doc §4
(identity model + registration chain + Builder), §5 (core node,
unchanged), §8 (tooling, separate module, not this branch).

Classes: **fresh** (written new against the spec, old file as
reference), **adapted** (localized edits, logic survives),
**untouched** (must not change — drift here means a mistake),
**ported** (tests: moved to the new shape, never rewritten from
scratch — each failure is a decision consciously revisited).

## Root package

| file | class | notes |
| --- | --- | --- |
| register.go | **dies → fresh** | replaced by `catalog.go` (`NewRegistration[T,C]`/`NewBareRegistration[T]` → chain Alias/Provides/Metadata/Hidden/System → **`.Register()` terminal** commits; completeness incl. required alias checked AT COMMIT; `Solo` is the second terminal) and `identity.go` (`CoreID = "sxcli.dev/fw"` identity + `CoreAlias = "core"` = config.CoreID; id/alias validators) |
| — (new) builder.go | **fresh** | `Builder()`, Accept/AcceptAll/Order/Alias, `Build() (*App, error)`, `Main()` terminal; composition-time checks (missing/colliding aliases, dup ids, concrete-type-once, ambiguity) |
| — (new) app.go | **fresh** | sealed `App`: owns what `runtime` + package globals own today; `App.Main()` |
| — (new) solo.go | **fresh** | `Solo(*Registration[T])` = accept-one + Main |
| main.go | **adapted (heavy)** | run/dispatch/usage/plan/execute/lifecycle/prepareTranslator/coreRoot become App methods; dispatch matches **aliases** (all of them, primary in listings); listings follow Order; coreRoot ids vs aliases per Subtleties |
| runtime.go | **adapted** | `runtime` folds into/behind `App` (it already is the instance shape; the composition made it public-worthy) |
| metadata.go | **adapted** | `Metadata`/`FieldMetadata` types stay; checkMetadata becomes registration-chain validation against the statically known `C` |
| introspect.go | **adapted** | ArgInfo.Service = **alias**; Services/Describe expose id+alias (shape TBD at mechanics); synthesized core uses identity+alias pair; SingleApplet unchanged |
| suppress.go | **untouched** | Suppress-under-Builder is an OPEN item — do not drag it in |
| tr.go | **adapted (minimal)** | `activeTranslator` stays package-level: only one App RUNS per process even if many are Built (tests Build several, run one at a time) — accepted wart, see Globals |
| types.go | **untouched** | interfaces are the stable contract |
| types_windows.go | **untouched** | |
| binpath.go, platform_*.go, pinned_open*.go | **untouched** | platform + hardening: paid-for scars |

## Internal packages

| package | class | notes |
| --- | --- | --- |
| internal/registry | **adapted (heavy)** | becomes the catalog: descriptors carry factory + accessor + aliases, `Instance` is nil until Build; `Virtual` stays; validation split per spec (types at registration, instances at Build) |
| internal/graph | **adapted (localized)** | two changes only: candidates follow the **composed rank order** (Build hands members ranked; the graph stays ignorant of Order itself), and unranked single-valued ties become a **violation** instead of silent first-wins — the "never silently" rule lands here |
| internal/config | **untouched*** | framework-ignorant by decree — the extraction guarantee; section names and env prefixes are caller-supplied strings and the root now passes aliases. *One surfaced exception (per the "surface it" rule): an additive `ProbeType(reflect.Type)` beside `ProbeFields` so type-level metadata checks can run at the `.Register()` commit with NO instance (the value-level default-in-domain check keeps `ProbeFields` at Build). Still framework-ignorant |
| internal/logging | **untouched** | |
| internal/fail | **untouched** | |

## Ecosystem packages (in-repo)

| package | class | notes |
| --- | --- | --- |
| sink/console | **adapted** | `const ID = "sxcli.dev/fw/sink/console"`, chain form, `.Alias("console")` |
| sink/file | **adapted** | same; alias `logfile` (operator contract — do not rename) |
| sink/syslog | **adapted** | same; alias `syslog` |
| configfmt/yaml | **adapted** | same; alias `yaml` |
| testdata/scmbox | **adapted** | new registration + Builder in its main |

## Test ledger (ported, one by one — this list is the completeness check)

Root: z_catalog (NEW — commit checks, deferred Make, chain
semantics), z_main, z_register (largely superseded → port what
survives, new catalog/builder tests replace the rest), z_core,
z_translator,
z_visibility, z_introspect, z_metadata, z_tr, z_suppress,
z_pinned_open, z_statregular, x_integration (personalities gain
Builder), x_scm_wine(+windows).
Internal: graph z_graph/z_inject (rank + ambiguity tests added),
registry (catalog semantics), config (untouched — must pass as-is),
logging.
Sinks/yaml: z_console, z_file, z_syslog, z_yaml (registration shape
only).

Rule: a ported test that no longer compiles or fails is a decision
being revisited — resolve it consciously, in the open, never by
deleting the assertion.

## Globals hit-list (grep before merge; each must be App-owned or consciously pardoned)

- `defaultRegistry`, `defaultCollector` → die into App/catalog
- `positionals` / `Positionals()` → pardoned this release (applet-facing
  API; single-running-app rule), open item for an injectable
  Invocation service
- `activeTranslator` → pardoned (single-running-app rule, documented)
- `Suppress`/`Enable`/`MaxConfigSize` state → untouched (open item)
- `Tr`/`TrN` → stay package-level by design (authoring surface)

## Subtleties (the misses waiting to happen)

1. **DECIDED: `fw.CoreID = "sxcli.dev/fw"` (identity), `fw.CoreAlias
   = "core"` (= config.CoreID — config section, operator surfaces).**
   The OLD exported `CoreID` ("core") renames to `CoreAlias` during
   coexistence — every old use site meant the alias. Do not "unify"
   them; different names on purpose. Completion's future
   HintServiceID filter targets `CoreAlias` (aliases are what
   operators speak).
2. Env prefix / config sections / `--disable` vocabulary derive from
   the **dispatched applet's alias** and each service's alias — the
   root passes aliases into internal/config everywhere it passed ids.
   The internals don't change; every call site's argument does.
3. Graph rank plumbing: Order must reach matching WITHOUT the graph
   learning about Builder — rank is expressed as the order of the
   member view Build composes. Keep the graph ignorant.
4. Ambiguity violation is NEW behavior — old tests encoding
   first-registered-wins (z_graph TestFirstRegisteredWins) are
   decisions to revisit, not bugs to fix.
5. Dispatch: selector matches ANY alias; listings show PRIMARY;
   basename matching also alias-based; Hidden/System semantics ride
   along unchanged.
6. `Iface[I]()` returns `reflect.Type` — it is the ONE bridge; resist
   adding more `any` seams.
7. The four committed rework phases (AlwaysOn, core node, subtree,
   exactly-once) are the BASE of this branch — their tests are part of
   the ledger and must survive.

## Order of work

1. ✓ catalog.go + identity.go + registry adaptation (fresh core of the
   model) — z_register/catalog tests ported alongside
2. ✓ builder.go + app.go + solo.go — Build validation + composition
   tests
3. ✓ main.go/runtime adaptation onto App — port z_main/z_core
4. ✓ graph rank + ambiguity — port z_graph/z_inject
5. ✓ alias sweep of operator surfaces — port
   z_visibility/z_introspect/z_translator/z_metadata
6. ✓ ecosystem packages + x_ suites + scmbox
7. the kill — old API dies, coexistence dies, docs truth-swept:
   - 7a ✓ config-tag validation joins the commit (ValidateConfigType)
   - 7b ✓ deletion: register.go gone; registry loses Register/Options/
     Check machinery/validateProvides/validateConfig/concrete map;
     config loses ValidateConfig; every test world speaks the chain
     (worlds via `w.build()`/`w.run()`, graph/registry/schema tests
     commit descriptors the way the chain does). Ledger decisions:
     TestOldStyleEntriesTolerated retired (the API it tolerated is
     gone; Build-side tolerance falls in 7c); Register-era id/instance/
     Provides/config violation tables superseded by z_catalog's chain
     checks; domain-default tests now assert at Build (type-level
     commit passes, run exits 2). Commit takes pre-collected Deps
     as-is — Build's copies keep catalog adjustments and tag
     violations are not double-reported.
   - 7c ✓ coexistence fallbacks dead: aliasesOf deleted (primaryAlias
     reads d.Aliases[0]; the chain guarantees one), config section is
     d.Aliases[0] with no id fallback, composedAliases drops the
     id-as-alias branch, buildFrom calls Make unconditionally
     (instance-carrying entries no longer ride through). The
     both-vocabulary controls (--enable/--disable/--override by alias
     OR id) are a FEATURE and stay.
   - 7d ✓ docs sweep: README front doors are Solo (single-applet) and
     Builder+ID-imports (composition); alias-derived surfaces (env
     prefix, config paths/sections) stated as such; spec's
     WithConfig/WithMetadata sentences now speak the chain. Code and
     docs grep clean of the dead API.

The branch is DONE pending merge. Left deliberately outside it:
sxcli-vet (separate module), completion-module bump (fw version +
HintServiceID filters CoreAlias), site resync, fwtest,
Suppress-under-Builder (open item). Release: the four rework phases +
composition ship as ONE breaking release.
