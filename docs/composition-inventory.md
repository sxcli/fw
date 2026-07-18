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
| register.go | **dies → fresh** | replaced by `catalog.go` (catalog, `Register[T,C]`, `RegisterBare[T]`, `Registration[T]` chain: Alias/Provides/Metadata/Hidden/System, registration-time checks) and `identity.go` (id/alias validation; CoreID split, see Subtleties) |
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
| internal/config | **untouched** | framework-ignorant by decree — the extraction guarantee; section names and env prefixes are caller-supplied strings and the root now passes aliases. Zero edits expected; any needed edit is a design smell to surface |
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

Root: z_main, z_register (largely superseded → port what survives,
new catalog/builder tests replace the rest), z_core, z_translator,
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

1. **`config.CoreID` stays `"core"` — it is the core's ALIAS** (config
   section, operator surfaces). The core's IDENTITY is a new constant
   (`sxcli.dev/fw`) living in the root. Do not "unify" them; they are
   different names on purpose. `fw.CoreID`'s meaning shifts — decide
   its export story at mechanics (completion filters by it).
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

1. catalog.go + identity.go + registry adaptation (fresh core of the
   model) — z_register/catalog tests ported alongside
2. builder.go + app.go + solo.go — Build validation + composition
   tests
3. main.go/runtime adaptation onto App — port z_main/z_core
4. graph rank + ambiguity — port z_graph/z_inject
5. alias sweep of operator surfaces — port
   z_visibility/z_introspect/z_translator/z_metadata
6. ecosystem packages + x_ suites + scmbox
7. globals grep, doc-comment sweep, spec stale-sentence pass
