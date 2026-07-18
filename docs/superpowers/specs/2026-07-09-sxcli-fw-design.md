# sxcli-fw — Design Specification

sxcli stands for **Simple Extensible CLI**.

Date: 2026-07-09
Status: approved section by section during brainstorming
Module: `sxcli.dev/fw` · Package: `fw` · Go: 1.26

## 1. Purpose & Scope

`sxcli-fw` is a Go framework for building busybox-style single-binary tools. A
consumer imports the framework and provider subpackages, registers services
(applets are just services implementing a specific interface), and gets a
binary with:

- dispatch by argv[0] (symlink style) or by first subcommand argument,
- unified argument / environment / config-file handling driven by per-service
  configuration structs,
- dependency injection between services,
- sequential, dependency-ordered lifecycle management,
- slog-based logging with pluggable sinks,
- a translation hook `Tr()`,
- Windows service (SCM) support.

### In scope for v1

- Full core: service model, registration, DI, closure resolution, dispatch,
  lifecycle, config/arg/env machinery, `Tr()`, `--help`, `--write-config`.
- One non-JSON config format provider: **YAML** (TOML will never ship in-tree;
  third parties may publish their own provider).
- Log sinks: console, file, syslog/journald.
- Windows service support (`SCMApplet`), tested via Wine + `x/sys/windows/svc/debug`.

### Deferred (designed around, not implemented)

- `ConfigurationUpdated` trigger semantics (interface reserved).
- Terminal UI provider (user-facing messages); only `Tr()` ships in core.
- i18n translation providers; gettext catalogs. `usage:` texts and `Tr()`
  format strings are the future extraction sources / msgids.
- Demo applet — to be decided; explicitly **not** based on busybox applets.
- Parsing/routing of positional arguments (they are collected but unrouted).

## 2. Module Layout

```
sxcli-fw/
├── go.mod                — module sxcli.dev/fw
├── *.go                  — package fw: the entire public API
├── platform_unix.go      — args acquisition, no-op service hooks
├── platform_windows.go   — SCM integration (svc.Run, handler, SCMApplet)
├── conf/                 — the config engine: schema, file discovery,
│                           source merging, arg/env parsing (public — the
│                           future sxcli.dev/conf, § below)
├── internal/
│   ├── registry/         — service descriptors, registration validation
│   ├── graph/            — closure resolution, topological order
│   └── platform/         — platform layer internals if they outgrow root files
├── sink/
│   ├── console/          — slog.Handler → terminal (opt-in sink)
│   ├── file/             — slog.Handler → file
│   └── syslog/           — slog.Handler → syslog/journald
└── configfmt/
    └── yaml/             — YAML ⇄ JSON config format provider
```

Consumers choose what links into their binary by importing packages
for their exported id constants and composing explicitly (§4 — the
composition model; imports catalog, `Accept` composes, nothing is
blank):

```go
import (
    fw "sxcli.dev/fw"
    "sxcli.dev/fw/sink/console"
    "sxcli.dev/fw/configfmt/yaml"
)

func main() {
    fw.Builder().AcceptAll().Order(console.ID, yaml.ID).Main()
}
```

Providers only import the root package — no import cycles. Everything under
`internal/` is invisible; the root package is the entire public contract.

## 3. Service Model — Interfaces

All in package `fw` (Go-conventional `-er` names):

```go
// Base lifecycle interface.
type Stopper interface {
    Stop() error
}

// Anything startable must be stoppable.
type Starter interface {
    Stopper
    Start() error
}

// Implemented by services owning a Configuration struct. The core fills the
// registered struct in place, then calls Configured() as notification —
// there is never a second config instance.
type Configurable interface {
    Configured() error
}

// Reserved in v1 — trigger semantics deferred.
type ConfigurationUpdater interface {
    ConfigurationUpdated() error
}

// Dispatchable entry point. Must be Configurable; must NOT implement
// Starter/Stopper — the application lifecycle brackets its Run.
type Applet interface {
    Configurable
    Run() int
}
```

In `platform_windows.go` (references `x/sys/windows/svc` types):

```go
// An applet that can run under the Windows Service Control Manager. It
// extends Applet: started as a normal process the framework drives Run
// as usual; under the SCM it drives Execute instead — one applet, both
// launch modes (console-mode debugging of services comes for free).
type SCMApplet interface {
    Applet
    Execute(args []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (svcSpecificEC bool, exitCode uint32)
}
```

### Enforced rules

- An applet (implements `Applet`; `SCMApplet` extends it) that also
  implements `Starter`/`Stopper` is a registration error.
- `Configured()` is called in dependency order on every closure member that
  is `Configurable`; `Start()` in dependency order on every `Starter`;
  `Stop()` in exact reverse order of the *successful* Start calls.
- Lifecycle calls are sequential — no concurrency in the core.
- Dependency cycles are legal but logged as warnings: injection is
  unaffected (all instances exist before anything runs), but the
  started-before-you promise cannot hold inside a cycle. Ordering uses
  the SCC condensation: the dependency guarantee holds *between*
  strongly connected components; *within* one, registration order
  applies. A cycle member may receive Start with an injected-but-not-yet-
  started dependency and must tolerate it.

### Windows service mode

`Main()` asks the platform layer whether the process runs under the SCM. If
so it calls `svc.Run` with a core-owned handler. The handler:

For testing, the same handler can run **outside** the service manager:
the `--scm-debug` argument (windows-only) enters `svc/debug` — console
process, Ctrl+C/Break translated to Stop/Shutdown requests. It is
argument-only by construction (a platform-level pre-scan, never a
config/env value, absent from `--help`) and **default-off**: a binary
exposes it with `fw.Enable(fw.FeatureSCMDebug)`; without the opt-in the
token is rejected as an unknown argument, as it is on non-windows
platforms.

1. reports start-pending status immediately so the SCM does not kill the
   service during initialization (documented on `SCMApplet.Execute`),
2. receives the argument vector in its `Execute` — this is where args come
   from in service mode,
3. runs the standard pipeline (parse → resolve → configure → start),
4. delegates to the applet's `SCMApplet.Execute`, forwarding the SCM
   request/status channels so stop/shutdown/interrogate reach the applet,
5. after it returns: reverse-order `Stop`, final status to the SCM.

A dispatched applet that does not implement `SCMApplet` while running
under the SCM is a logged error, exit code 2.

## 4. Registration & Dependency Injection

DECIDED 2026-07-18, the composition release (implementation pending —
this section leads it): registration and participation are two
separate acts. `Register` fills a process-wide **catalog** — a phone
book of factories and declarations, holding no state and activating
nothing. What a binary actually *is* gets said explicitly, once, in
`main`, through the **Builder**. Blank imports die: a package is
imported for its exported id constant, which the composition
references — the import is justified by an identifier like any other.

### Identity: IDs and aliases

A service carries two names with two jobs — the old single flat id
served six masters and is retired:

- **ID — unique, namespaced, machine-facing.** The catalog key, the
  DI identity, what `Accept`/`Order`/`Builder.Alias` and inject-by-id
  tags reference. Convention: **the package's import path**, with a
  `/name` suffix when one package registers several services
  (`sxcli.dev/fw/sink/console`, `example.com/mytool/srv`). Uniqueness
  is inherited from Go's own module namespace — domain-owned,
  collision-proof by the same rules as imports — and provenance is
  legible in every error message. `sxclivet` verifies the exported
  `ID` constant begins with the package's import path (the runtime
  cannot know import paths; the guarantee is the tool's). Inject tags
  reference IDs — verbose, copy-pasteable, and correctly so: naming a
  specific service is a deliberate, concrete act.
- **Alias — short, human-facing, REQUIRED.** The CLI selector, the
  config-file section, the env-prefix source, the
  `--disable`/`--enable` vocabulary, the completion candidates.
  Declared explicitly at registration (`.Alias(...)` — deliberately,
  no derivation from the ID: a library author who types "logging"
  made a choice they can be blamed for; derived defaults would make
  it silently). Lowercase, digits and hyphens (env derivation
  uppercases and maps hyphens to underscores, so `cherry-pick` is
  finally a legal command name). Aliases need only be unique **within
  a composition**: a collision between accepted services is a `Build`
  violation, resolved by the composer. Convention, documented:
  libraries prefix (`acme-log`), applications go plain. A service may
  declare several (`.Alias("cherry-pick", "cp")`) — the first is
  primary (env prefix, config section, listings), all are selectable.

Operator-facing surfaces throughout this spec that historically said
"service id" — config sections, env prefixes, dispatch selectors,
control vocabulary, `HintServiceID` completion — mean the **alias**
from this release on; identity appears only in code and diagnostics.
The core itself follows the model: identity `sxcli.dev/fw`, alias
`core` — both reserved.

### Registration API

Registration is a builder with a terminal: the constructor carries the
type parameters (Go permits them only there — methods cannot be
generic) and returns a free-standing value; chain methods enrich it;
**`.Register()` commits it to the catalog** — construct freely, commit
completely. The catalog never holds a half-built entry, so
completeness (the required alias above all) is validated **at the
commit**, registration-time, all-at-once like every other violation.
A chain may end two ways: `.Register()` into the catalog for
composition, or consumed by `Solo` (the second terminal). A forgotten
terminal compiles — explicit `Accept` catches it loudly ("unknown
id"); `sxclivet` closes the silent `AcceptAll` case statically:

```go
func NewRegistration[T, C any](id string, factory func() *T, cfg func(*T) *C) *Registration[T]
func NewBareRegistration[T any](id string, factory func() *T) *Registration[T]
func Iface[I any]() reflect.Type // type token for Provides

// on *Registration[T]:
//   Alias(names ...string)      — REQUIRED; first is primary
//   Provides(types ...reflect.Type)
//   Metadata(md *Metadata)
//   Hidden()
//   System()
//   Register()                  — the terminal: validate + commit
```

```go
const ID = "sxcli.dev/fw/sink/console"

fw.NewRegistration(ID, newConsole, func(c *Console) *Config { return &c.cfg }).
    Alias("console").
    Provides(fw.Iface[slog.Handler]()).
    Metadata(&fw.Metadata{...}).
    Register()
```

Two entry points, split by the most structural fact about a service —
whether it owns a config struct — chosen over a single entry with an
erasing wrapper: two dumb names beat one clever indirection. The
config accessor rides the entry because that is where `C` can be
generic; the constructor's field values are the defaults, and the
framework applies the accessor to each `Build`-fresh instance.

The typed factory anchors the concrete type at registration, so
everything structural is validated with **no instance constructed** —
inject tags, applet-ness, Starter/Stopper rules, declared interfaces
(`reflect.PointerTo(T).Implements`), config tags and metadata keys
(through the statically known `C`). Constructors carry a contract:
**cheap** — allocate and set defaults, nothing else; I/O belongs to
`Configured` (the sink contract's philosophy, extended to birth).
Every registering package **exports its id** as a constant (`ID`;
`XxxID` when a package registers several) — the constant is the
package's public handle and what compositions reference; `sxclivet`
enforces the convention (§Tooling).

**Applet visibility** is registration-time policy, not a capability —
hence registration-chain methods, not interfaces (interfaces declare
what a service can do; the registration declares how the framework
treats it). The progression: a
plain applet is a public command; `Hidden` keeps it selectable by an
explicit first token but removes it from usage listings, the future
`--applets` enumeration and basename/symlink dispatch (hidden debug or
maintenance commands); `System` declares machinery of the binary that
a human is never meant to type — a shell-completion query endpoint is
the canonical case — and additionally excludes the applet from
single-applet counting, so a module registering one cannot flip an
existing binary's dispatch mode. `System` implies `Hidden`. Whether
the applet was cataloged by a library package or written in `main.go`
is irrelevant — the options describe what the applet *is*, not how it
got there: a library of ordinary applets registers them plain, and
once accepted they count and list like any command. In
every other respect Hidden/System applets are ordinary: id rules, the
`APPLETID_` env prefix, config files, closure resolution and the
lifecycle are unchanged.

**Metadata** is declarative — no interfaces on the instance or the
config struct (a service is the instance+config pair; one Metadata
covers both):

```go
type Metadata struct {
    Description string         // long-form service/applet description
    Fields      map[string]any // FieldMetadata[T] values, keyed by Go
                               // field name ("Level", "TLS.Cert")
}
type FieldMetadata[T any] struct {
    Allowed []T       // closed value domain; enforced by the framework
    Doc     string    // long-form field description; usage: stays the one-liner
    Hint    ValueHint // advisory: what the value denotes (HintFile,
                      // HintDirectory, HintServiceID) — for tooling,
                      // never enforced
}
```

A **Hint** declares what a value *denotes* so tooling (completion,
documentation) can act on it — `HintFile` says "this names a file",
which the core cannot and must not enforce: `--config new.yaml` names
a file that does not exist yet. Hints are the advisory sibling of the
enforced `Allowed` domain, in the same trust class as `Doc`.
`HintServiceID` says "this names a service registered in this binary"
— completable from the Introspector, again advisory (resolution may
still reject an unknown id with its own, better error). The core
dogfoods the mechanism: `--config` declares `HintFile`; `--disable`
and `--enable` declare `HintServiceID` (`--override` takes `from=to`
pairs — no honest hint fits).

Validated at registration with everything else: an unknown field key, a
non-FieldMetadata value, field metadata on a config-less service, an
Allowed element type that does not match the field's type (same kind
and convertible; element type for slices), a **registered default
outside its own declared domain**, a hint on a non-string field (paths
are strings), an unknown hint value, or a hint combined with a
non-empty Allowed on the same field (a closed enum and "it's a file"
contradict each other — declare one) are violations. Description alone
is fine for config-less services.

A non-empty `Allowed` is **enforced, not advisory**: every write path —
strict argument parse, environment application, config file
application — rejects a value outside the domain as a loud startup
violation naming the source and the allowed set (slice fields checked
per element). Services may keep their own checks as defense in depth,
but the declared contract is honored by the machinery, and completion
services can trust it via `ArgInfo.Allowed`; the advisory hint travels
the same road as `ArgInfo.Hint`.

`Register` is typically called from package `init()` — cataloging is
the one thing init still does, and it is inert; a single-file tool may
register straight from `main` and have no init magic at all. One
package may register many services.

**Validation is two-phase, each phase checking what exists at that
moment.** Registration validates types and declarations (never
panics; violations are recorded and reported all at once):

- malformed ID (path-shaped, lowercase; `sxcli.dev/fw` and the core's
  Introspector id are reserved — import-path *equality* is `sxclivet`'s
  check, the runtime cannot know import paths),
- malformed alias (lowercase, digits, hyphens, starts with a letter),
- declared interface the concrete type does not implement,
- applet implementing Starter/Stopper,
- malformed tags / unsupported config field types,
- `inject` tag on an unexported field,
- slice-of-concrete-struct `inject` field,
- `Hidden`/`System` on a non-applet,
- metadata violations that are type-level (unknown field keys, type
  mismatches, hint rules).

`Build()` validates the composed, instantiated reality: **colliding
aliases** among the accepted (a missing alias is caught earlier, at
the `.Register()` commit), the same concrete type accepted twice,
unresolvable required dependencies, unbroken ambiguity (below), and
the value-level metadata check — a constructor default outside its own
declared domain — which needs instances to exist. (Duplicate ids are
a catalog-commit violation: two packages claiming one id is wrong
before any composition exists.)

The concrete struct type is recorded automatically — no option needed —
so dependents may require the service by `*Struct` or by any declared
interface.

### Composition: Builder, Accept, Order

```go
app, err := fw.Builder().
    AcceptAll().                            // admission: what the binary is
    Order(cat.ID, ls.ID, console.ID).       // ranking: who wins, who lists first
    Alias(acmemetrics.ID, "acme-metrics").  // composition-level rename
    Build()                                 // instantiate + validate, all at once
app.Main()                                  // never returns

fw.Builder().AcceptAll().Main()             // terminal form: Build, report
                                            // violations, exit 2 — the standard
                                            // startup contract

fw.Solo(                                    // the single-applet front door:
    fw.NewRegistration("example.com/mytool/srv", newSrv,
        func(s *Srv) *Config { return &s.cfg }).
        Alias("srv"))                       // Solo is the second terminal:
                                            // commit + accept + Main
```

Two independent axes, two verbs:

- **`Accept(ids...)` / `AcceptAll()` — admission.** What the binary is
  composed of. An un-accepted catalog entry does not exist for this
  app: no resolution, no dispatch, no introspection, and no `--enable`
  — `Accept` is the developer's composition boundary (linking);
  `enable`/`disable` remain the operator's runtime controls *within*
  it.
- **`Order(ids...)` — rank among the accepted.** `Order` never admits:
  ordering an un-accepted id is a composition violation (a free typo
  catcher). Ranked beats unranked in single-valued matching; slice
  fields gather ranked members first in `Order` sequence, then
  unranked **sorted by id**; `Order` also drives listing order (usage,
  `Applets()`, help sections). Multiple `Order` calls append.
- **`Alias(id, names...)` — composition-level rename.** Overrides what
  an accepted service answers to, for this app only, upstream
  untouched: `Builder.Alias` > registration `.Alias(...)` —
  registration proposes, composition disposes. All operator surfaces
  follow the composed result. Beyond collision-fixing, this is how
  **the composition owns the operator contract**: a released binary
  pins the aliases it has documented, and no upstream rename — or
  even provider swap — ever touches a deployed config file again.
  Recommended practice for released binaries.

**Ties are never broken silently** — the one rule, everywhere. A bare
single-valued field with two candidates neither of which is ranked is
a composition violation naming both; resolution is an id in the inject
tag or an `Order` entry. Import order, registration order and
`goimports`' opinions appear nowhere in the semantics: a formatter can
never again change which service gets injected.

`Build()` runs every accepted factory exactly once (constructors are
cheap by contract), applies the config accessors, validates, and
returns a **sealed** App — composition immutable from here, the graph
immutability philosophy one layer up. The error return is for tests
and embedders (`fwtest` composes, builds, asserts); `Main()` — on the
App or as the Builder terminal — is the production path and reports
violations under the standard all-at-once, exit-2 contract. Multiple
Builds in one process are legal and share nothing: each App owns fresh
instances, which is what makes user tests finally clean (no shared
state, no filled-config-becomes-defaults corruption).

The gradient is deliberate: `Solo` for one applet and no composition
questions; `AcceptAll` for busybox assembly with ambiguity refused
loudly; `Accept`/`Order` when the composition itself is the point.
Nothing learned at one tier is unlearned at the next — `Solo` *is* the
builder route, pre-composed.

Ejection is unchanged in spirit and simpler in mechanism: closure
resolution decides lifecycle as always, and accepted-but-cold
instances are released after `Retain` — born cheap at Build, dropped
before `Configured` ever runs.

### Introspection

The core registers exactly one **`Introspector`** service itself, under
the reserved id `introspection` (reserved like `core`: the id is
rejected for any other concrete type, and registering the core's type
under another id collides with the core's own registration — squatting
always fails startup loudly). It is the read-only composition view for
services implementing completions, documentation generators and similar
meta features *outside* the core; there is exactly one because it
reports composition truth, and truth does not federate. Services may
not provide their own introspectors — future extensibility goes the
other way: optional self-description interfaces whose data the single
core Introspector aggregates (see Open Items).

Consumers inject it by concrete type (`*fw.Introspector`), cold like
any service. **A closure containing the Introspector is never
ejected** — enumerating the binary requires the registry alive; only
invocations that injected it pay that.

Surface: `Applets()` (public applets only — `Hidden` and `System`
applets are omitted: a completion must not offer what a human should
not type), `SingleApplet()` (the applet that would run with no
selector word — dispatch-mode truth from the dispatch rules
themselves; consumers must not re-derive it from `Applets`, which is
public-only while a Hidden non-System applet still counts for the
mode), `Services()` (every registered service — plus `core`,
synthesized: the core is a virtual root, not a registry entry, but it
is truthfully part of every binary), `ConfigExtensions()`,
`Describe(serviceID)` (the registration Metadata's long-form
description; `Describe("core")` answers a fixed description), and
`Arguments(appletID, args) ([]ArgInfo, error)` — the closure-true
argument schema the applet would have if invoked with `args`. It runs
the real planning pipeline (the shared `plan()` also used by
execution, so introspection truth cannot drift): lenient core peek
honoring an in-line `--config`, file loading, controls from every
source, closure resolution, schema
construction — with **zero side effects**: nothing is written, ejected
or mutated; `--write-config`/`--help` inside `args` are inert data
(beyond the write-config missing-target source selection). Callers
pass the words *before* the completion cursor — a half-typed token
passed as data would be planned as configuration. The result is
best-effort: on planning violations, `Arguments` retries with no files
and no controls (the registration-level schema) and returns it
alongside the joined error.

### Dependency declaration

A struct tag on exported fields of the registered *instance*:

```go
type MyService struct {
    Log     slog.Handler    `inject:""`            // by interface: sole match, or the ranked one
    Sinks   []slog.Handler  `inject:""`            // by interface, ALL registered (all enter closure)
    Chosen  []slog.Handler  `inject:"id1,id2"`     // listed IDs seed the closure
    Store   *BoltStore      `inject:""`            // by concrete type (unique)
    Extra   slog.Handler    `inject:";optional"`   // optional: nil if absent
}
```

Tag value grammar: `"<id>[,<id>...][;optional]"`.

- Empty ID list → match by field type. IDs given → match by type *and* ID.
- Interface field types match only services that *declared* `Provides` of
  that interface — accidental structural matches never inject.
- Pointer-to-struct fields match by concrete type (unique by rule above).
- Single-valued field → the named ID; with no id: the sole match, or —
  when several match — the one the composition **ranked** (`Order`).
  Among several ranked candidates, **`Order` sequence decides** —
  explicitly, in source: `Order(a, b)` means a beats b wherever both
  match, the same sequence that orders slices and listings.
  Multiple matches with no ranked candidate is a resolution violation
  naming both claimants: **ties are never broken silently** — the
  first-registered rule of the pre-composition era is dead, and with
  it the import-order hazard. Applies to `;optional` fields equally.
  A single-valued field may name **at most one** id — a list on a
  non-slice field is a registration error (see Open Items for the
  planned boolean preference syntax).
  Convention for such runtime violations framework-wide: when
  `sxclivet` would have caught the mistake statically, the message
  says so — the error text is the tool's advertisement.
- Slice fields: **interface element types only**. Injection delivers *all
  enabled* matching services in registration order — with listed IDs the
  slice may contain *more* than listed (seeded services and other
  closure members of that type are included too). A type-only slice pulls
  **every** registered service of that type into the closure; listing IDs
  is how to narrow that.
- `;optional`: zero matches leaves a nil field / possibly-empty slice.
  Without it, zero matches is a startup error.
- Optional tolerates a *disabled* target, never an *unknown* one: an id
  that names no registered service is a startup error even on an
  optional field — a typo must never silently change the composition.

## 5. Dispatch & Application Lifecycle

### Entry point

```go
func Main() // no args, never returns (calls os.Exit)
```

No parameters by design: the argument vector is platform-sourced (POSIX:
`os.Args`; Windows service mode: the vector the SCM hands to `Execute`).

### Dispatch rules

1. Obtain argv from the platform layer.
2. **Single-applet mode:** if exactly one non-`System` applet is
   registered, it is always dispatched. argv[0] is ignored and *no
   applet-selector consumption happens* — the entire argument vector
   (after the binary name) belongs to the applet as ordinary
   flags/positionals — with one carve-out: a first bare token equal to
   a registered `System` applet's id selects that applet instead
   (rule 3 applies to it). Consequence, documented: with `System`
   applets present, not every start of the binary runs the main
   applet, and a genuine positional colliding with a `System` id needs
   the standard leading `--` escape. This lets the framework serve
   simple applications with no thought given to binary names,
   symlinks, or subcommands. Notes:
   - `binary myapplet --args` does **not** treat `myapplet` as a selector
     even when it matches the sole applet's ID — it is a leading bare
     token under normal arg parsing. Selector logic is off for the main
     applet; only `System` ids are consulted, so there is no
     "data or selector?" ambiguity beyond that carve-out.
   - Registering a second non-`System` applet re-enables selector logic
     (rules 3–4),
     changing the binary's command-line contract. That breaking change is
     the developer's responsibility to manage.
   - Only dispatch changes. The sole applet's ID still anchors the
     `APPLETID_` env prefix and config file names; closure resolution,
     disable/enable/override, and the lifecycle proceed as usual.
3. **If the first argument exists and does not start with `-`:** it is
   always an applet selector. Look it up, dispatch with the remaining args,
   overriding argv[0]. Unknown name → dispatch failure — even if
   basename(argv[0]) is itself a valid applet. No fallback. `Hidden`
   and `System` applets are selectable here like any other — explicit
   selection always works; only listings and basename matching exclude
   them.
4. **Otherwise:** basename(argv[0]) must name a registered non-`Hidden`
   applet; dispatch with all args (`Hidden`/`System` applets are never
   matched by basename). On Windows the `.exe` suffix is stripped before
   matching.
5. Every dispatch failure (including a binary with zero registered applets)
   prints usage — including the list of registered applet IDs, `Hidden`
   and `System` ones omitted — to stderr
   and exits non-zero. In single-applet mode the applet list is dropped
   from the usage output. `--help` renders only the dispatched applet's
   argument schema (core + closure, grouped by service ID) and never an
   applet list; enumerating applets is a future core argument (see Open
   Items).

Consequence (documented): in multi-applet binaries a leading bare token is
*never* applet data. Scripts must know `binary appletName --args`
dispatches to `appletName`, and a symlinked applet cannot take a bare first
positional. In single-applet binaries the same reservation exists only
for `System` ids — the intended consumers (shell completion scripts)
call `binary systemid --args` and never rely on basename dispatch.

### Pipeline

```
init() registrations → Main()
  1. validate registry (all recorded errors reported at once)
  2. dispatch → applet ID known (env prefix APPLETID_ fixed from here)
  3. first-pass LENIENT parse of the core's own config from args/env
     (--config, --write-config, disable/enable/override, --help, …);
     unknown arguments ignored in this pass
  4. config file discovery and loading (format providers used pre-lifecycle
     as pure stream transforms)
  5. closure resolution, rooted at the core node (below): the core's
     inject fields — the dispatched applet, the registered translator
     (optional), the format providers in use — plus transitive deps,
     with disable/enable/override applied; dependency-ordered via SCC
     condensation (cycles are warnings, registration order within);
     cold services are ejected from the registry so their instances
     can be garbage collected
  6. strict full parse — complete arg/env schema now known
     (core + every closure member); unknown argument = error
  7. fill each closure member's config struct (in place, merged values)
  8. inject dependency fields
  9. Configured() on each Configurable, dependency order — the
    registered Translator's dependency subtree always first (§7),
    so every later Configured may already render translated text
 10. assemble log multihandler, replay startup buffer, swap slog default
 11. Start() on each Starter, dependency order, sequential
 12. code = applet.Run()          (SCM mode: applet.Execute(...))
 13. Stop() on each started Starter, reverse start order
 14. os.Exit(code)
```

Both short-circuit paths below first run Inject + `Configured` for
the registered Translator's dependency subtree (§7) — the only
lifecycle they execute — so their output renders translated; a
translator failure degrades quietly to msgids and the short-circuit
proceeds.

`--write-config` short-circuits after step 7's merge: write the merged
config to the `--config` target (format chosen by the file extension via a
format provider) or, with no target, dump JSON to stdout; exit 0 without
Configured/Start/Run. The target is input *and* output: an existing
target is loaded as the explicit config first — making `--write-config`
an easy way to normalize/reformat an existing file — and a missing one
is only created. **Newly created** files get mode 0600; an existing
target's permissions are the operator's prior decision and are left
untouched (format normalization must not silently revoke a
deliberately granted group read). Empty values — zero scalars, empty
slices — are skipped, and sections or nested objects they would leave
empty are omitted entirely, so a default-heavy configuration dumps
small. Consequence: a field explicitly set to its zero value is
indistinguishable from an unset one and falls back to its default when
the dump is loaded.

`--help,-h` (core-owned) prints the dispatched applet's full argument schema
— core + entire closure, grouped by service ID, with usage texts (rendered
through `Tr()`) and the **current effective values** (all sources already
merged: what the binary would actually use) — to stdout and exits 0.

### The core node

Resolution roots at the framework core itself, modeled as a real
dependency-bearing node. The core's needs are not seed lists bolted
onto the resolver — they are **inject fields like everyone else's**,
on a per-invocation virtual root the runtime composes dynamically
(`reflect.StructOf` with `inject` tags):

- the **dispatched applet**, by id, typed with its concrete struct
  type (concrete-type matching is automatic — no interface declaration
  exists or is needed for applets);
- the registered **Translator**, optional by interface: present means
  pulled into the closure, absent means fine (the exactly-one rule of
  §7 is checked separately);
- one field per **format provider in use** this invocation (extension
  matched an actually loaded file or the `--write-config` target), by
  id.

The registry's own tag machinery collects these dependencies and the
graph matches and injects them unmodified — the core consumes services
through the same mechanism it offers everyone, and after injection the
core's fields ARE the runtime's references. The node joins the ordered
closure (last — it depends on everything) and is inert in the
lifecycle: the composed struct has no methods. Seeds are gone as a
concept; what used to be seed lists (AlwaysOn — removed, provider
seeds, the translator seed) are now visible dependency edges.

Why a *virtual* root rather than a registry entry: the Introspector's
`Arguments` re-plans freely — different applets, different provider
sets, a different core struct each pass — and `reflect.StructOf`
returns identical types for identical field sets, colliding with both
the duplicate-id and duplicate-concrete-type rules. The graph
therefore takes the root descriptor as a parameter; the registry never
holds it. Introspection **synthesizes** the core's visibility instead:
`Services()` includes `core`, `Describe("core")` answers a fixed
description — truth for humans, one truth (the graph) for the
machinery.

The boundary stated once, plainly: the core's **config** (`--config`,
`--disable`, `--enable`, `--override`, …) *drives* resolution and is
parsed before the graph exists — it stays the pre-graph machinery it
is today. The core node is the graph-and-injection side only.

### Config-driven service control

Part of the core's config struct (settable via args, env, or file like
everything else):

- `disable` — service IDs removed from the closure even if required.
  Disabling the dispatched applet itself is a startup error, as is
  listing the same id in both `enable` and `disable`.
- `enable` — service IDs forced into the closure (with their transitive
  dependencies) even if nothing requires them.
- `override` — ID remapping (`sqlite=mysql`): wherever a dependency names
  `sqlite`, resolve `mysql` instead. The substitute must satisfy the
  dependency's field type (checked at resolve time) or startup fails.
  A disabled required dependency without a substitute is a startup error.
  An override key may name an unregistered id (rescue mappings for
  unlinked implementations), and an override matching no dependency is
  legal — generic configs may carry entries irrelevant to this applet —
  but each unused key is logged as a **warning**, so a typo never
  silently changes nothing.

This is why closure resolution (step 5) happens *after* config loading
(steps 3–4): enablement is configuration-driven (e.g. an applet declares a
SQLite database service but the user configures MySQL instead).

### Failure semantics

- Any error before `Run` (config parse, resolution, Configured, Start)
  aborts startup; services already started get reverse-order `Stop` first;
  buffered logs flush to stderr; process exits with the framework exit
  code **2**.
- `Stop` errors during any shutdown are logged, never change the exit code,
  and never prevent the remaining Stop calls.
- **Applet panics are not recovered.** The applet owns its own recovery and
  must return its error code from `Run()`.
- SCM mode maps failures to the appropriate SCM exit status.

## 6. Configuration System

### The config struct

Declared by the registration chain's config accessor
(`NewRegistration[T, C]`'s third argument); the factory's field values
are the defaults; the core fills the same struct in place before
`Configured()`.

```go
type FileSinkConfig struct {
    Path    string        `json:"path"    arg:"log-path"     usage:"log file location"`
    Level   string        `json:"level"   arg:"log-level,l"  env:"LOG_LEVEL" usage:"minimum level"`
    MaxAge  time.Duration `json:"maxAge"  arg:"log-max-age"  usage:"rotation age"`
    Backups int           `json:"backups"`                   // no arg tag → file-only
}
```

- `json:"…"` — **required** on every exported field; the core is JSON-native.
  File keys nest under the service ID: `{"filesink": {"path": "…"}}`.
  The core's own config lives under the reserved ID `core`.
- `arg:"long[,short]"` — explicit opt-in per field; no tag → no CLI
  argument. Duplicate long names across the closure = startup error;
  short names are first-come-first-served.
- `env:"NAME"` — an explicit opt-in of its own: a field with only an
  `env` tag is env+file settable (useful for values deployable via
  environment without cluttering the CLI, e.g. tokens). When `arg` is
  present and `env` absent, the name derives as `APPLETID_` + long name
  uppercased with dashes → underscores (applet `cat`, arg
  `log-max-age` → `CAT_LOG_MAX_AGE`). `env:"-"` suppresses the env var
  entirely, derivation included — combined with `arg` it makes a field
  argument-only (how the core's `help`/`write-config` are locked down).
  A field with neither tag is file-only.
- `usage:"…"` — help text; rendered through `Tr()`; doubles as a gettext
  extraction source when translation support lands.

Supported field types (v1): `string`, `bool`, all int/uint widths, floats,
`time.Duration`, and slices of these. Nested structs are allowed for
file/JSON structure but their fields are file-only (no `arg`/`env` tags) in
v1. Anything else (maps, custom types) is a registration error.

### The conf module & the unified tag grammar (direction, 2026-07-18)

The configuration system's endgame is **`sxcli.dev/conf`** — the
engine above, standalone, aimed at replacing cobra/viper for
single-command tools (dispatch is fw's territory; the busybox model IS
the multi-command story). The pitch against viper: no store, no
watching, no package-global mutable config object — the struct is the
schema, filled once, immutable for the run, strict about unknowns.
The ladder: (1) in-tree decoupling — `NewSchema` takes neutral
`Section{Name, Ptr, Meta}` members and the registry import dies; (2)
in-tree pipeline promotion — the discover→peek→lenient→env→strict→fill
sequencing moves from fw's `plan()` into a conf front door fw then
calls; (3) the module split at the v1 horizon, not before — a public
surface frozen early ossifies before the model has proven itself.

Decided:

- **Dedicated/total stays the model.** Every exported field
  participates and carries `json:`; totality — not provenance — is
  the requirement, and it is what keeps unknown-key strictness and
  `--write-config` completeness honest. (A per-field exclusion tag is
  the escape hatch if adoption friction ever demands one; adding it
  later breaks nothing.)
- **No flattening, ever.** An embedded struct is an ordinary nested
  path element. Env derivation generalizes to **paths**: derived name
  = section alias + the field's path segments, joined with
  underscores. A top-level field's segment is its conf name, so every
  currently derived name survives unchanged; nested and untagged
  fields *gain* derived names (segments from json names).
- **Derived-name collisions are Build errors** naming both fields —
  underscore joins make distinct paths collidable — with the vet
  nudge; sxclivet reports the same collision at compile time (§8).
- **One operator name.** `arg:` dies; **`conf:"long[,short]"`** names
  the field's whole operator surface: it grants `--long`/`-s` AND
  feeds env derivation — no more env names silently downstream of a
  flag rename. `env:` owns the env axis in all its states: absent →
  derived; `env:"NAME"` → **verbatim global**, no alias prefix (its
  only remaining job is matching names you don't own — `HTTP_PROXY`,
  `NO_COLOR`; a prefixed custom name is just `conf:`); `env:"-"` →
  none. Explicit env is a strong claim: collisions are Build errors a
  composition cannot rename away (`Builder.Alias` repins derived
  names only) — reserve it for names you match, never names you
  invent; a Builder-level env remap stays in the back pocket.

The matrix (every row a real use case, no dead cells):

```go
Listen  string `json:"listen"  conf:"listen,l"`               // file + --listen/-l + ALIAS_LISTEN
Backups int    `json:"backups"`                               // file + ALIAS_BACKUPS (derived, no flag)
Token   string `json:"token"   env:"MYAPP_TOKEN" dump:"-"`    // env ONLY — secrets touch neither argv nor files
Proxy   string `json:"proxy"   conf:"proxy" env:"HTTP_PROXY"` // --proxy + verbatim global env
Help    bool   `json:"help"    conf:"help" env:"-" dump:"-"`  // arg-only, run-scoped
Legacy  string `json:"legacy"  env:"-"`                       // file-only
```

The Token row extends `dump:"-"` beyond run-scoped flags to its
general meaning — **not file material**: excluded from
`--write-config` output and refused loudly when a file supplies it.
Secrets never touch argv (`ps`-visible) and never belong in config
files (backed up, committed, world-readable more often than not); the
framework refuses to normalize either. `json:` stays required even
here — refusal needs the key name to refuse by.

Open (recorded, undecided): the composite-core shape (the core
section as ONE namespace fed by two flat structs — conf's knobs +
fw's controls — proposed, unblessed); the camel-split rule for
json-derived env segments (`maxAge` → `MAX_AGE`); whether `conf:` at
depth is a violation like `arg:` today (proposed: yes — args are the
flat, scarce surface; files and env address paths); whether a
dedicated secret marker should replace the `dump:"-"` reuse; the
`arg:`→`conf:` tag cutover timing (before the release train departs,
so the breaking release stays ONE).

### Config schema versioning & migration (decided 2026-07-18)

Schema evolution is **developer-owned, typed, and runtime** —
Kubernetes-style API conversion applied to config, not metadata the
engine interprets. The engine stays dumb: it walks a chain.

- **Every config struct mandates `Version uint32`** (json-tagged like
  everything — totality). The factory default carries the current
  number, so `--write-config` emits it for free.
- **Old versions live on as plain types** — and only as *file
  schemas*: `json:` tags, no `conf:`, no `env:`. Args and env always
  speak the current dialect (operators spell them against the current
  binary's `--help`); only files age.
- **The chain is typed steps declaring their from-version**:

  ```go
  NewRegistration(ID, newServe, accessor).
      Alias("srv").
      Migrate(
          conf.Step(1, func(old ConfigV1) ConfigV2 { … }),
          conf.Step(2, func(old ConfigV2) Config { … }),
      ).
      Register()
  ```

  Generics keep each link fully typed; erasure happens inside `Step`.
  The **commit validates the chain** — contiguous from-versions, each
  link's output type feeding the next link's input, terminating at
  the current config type — type-level work that belongs to the
  registration commit under two-phase validation; sxclivet mirrors it
  at compile time. No chain registered = versions must match exactly,
  mismatch = loud error: never-silent by default, ceremony only when
  a schema actually evolves.
- **Load flow, per section instance, per file**: `version` == current
  → normal strict parse. `version` < current → **strict-parse against
  that version's struct** (strictness survives history — unknown keys
  in an old file are judged by the old schema), walk the chain, and
  the result enters the merge as that file's values. `version` >
  current → hard error ("config written by a newer version" — the
  binary-older-than-file case gets its own message). Missing →
  current dialect, partial.
- **Version implies complete.** A section carrying `version` is a
  complete document, migrated whole; a versionless section is a
  partial in the current dialect, merged key-by-key. The convention
  is self-enforcing: the only producer of versioned files is
  `--write-config`, which writes complete documents, while
  hand-written partials never include a version key. Typed conversion
  functions cannot see key *presence* (a partial unmarshaled into an
  old struct makes absent keys indistinguishable from zero values) —
  this convention is what dissolves that, and the only combination it
  declines to support is a *stale-versioned partial*: the file nobody
  has a reason to write. The completeness rule only bites when
  `version` < current — a trimmed current-version file still merges
  as a partial.
- **Migrate first, merge second.** The version is a property of a
  section instance in one file, never of the merged result: each
  file's section is independently brought to the current dialect,
  then the normal location-order presence merge runs. Different
  versions across files — and different versions across *sections* of
  one file — are the expected steady state (sections belong to
  different packages evolving at different speeds; a file-level
  version could never work). Two complete documents at different
  versions shade each other exactly as two complete documents do
  today: precedence is precedence.
- **Checks run on migrated values**: domain/metadata enforcement
  applies to the *output* of the chain against the current schema — a
  migration cannot smuggle in an out-of-domain value.
- **Pipeline consequence** (pins the promotion work): per-file parse
  → version peek (the `PeekCore` trick, per section) → strict-check
  against that version's schema → migrate → merge. Migration is why
  the pipeline moves into conf as a designed thing, not lifted as-is.
- `--write-config` completes its arc as the **migration normalizer**:
  load old file → migrate → emit a complete current-version document.

Open within the scheme: where `Migrate` lives (on the fw registration
chain as sketched, forwarding to conf's `Section`, or conf-only);
whether a chain may start above 1 to drop ancient versions (proposed:
yes — older than the oldest step = loud "version N no longer
supported" error, it is just the chain not reaching back).

### Sources & precedence (least → most important)

```
defaults  <  config files  <  environment  <  arguments
```

Config files, loaded in order, later overriding earlier field-by-field:

1. `<binary-dir>/<applet-id>-config.<ext>` (next to the real binary)
2. `/etc/<applet-id>/config.<ext>` — on Windows:
   `%ProgramData%\<applet-id>\config.<ext>` (`C:\ProgramData` when the
   variable is unset)
3. XDG user config location: `<xdg-config>/<applet-id>/config.<ext>` —
   `os.UserConfigDir` per platform (`%AppData%` on Windows)

**Security — the binary-companion location (1) is pinned:**

- "Next to the binary" means next to the **real** binary: the executable
  path from `os.Executable()` with every symlink resolved. Busybox-style
  applet symlinks never relocate the companion location — a symlink to
  the binary in an attacker-writable directory must not choose its
  configuration.
- The companion itself is opened refusing a symlink at the final path
  component (`O_NOFOLLOW`, atomically enforced by the kernel — no
  check-then-open race; the Windows variant rejects reparse points
  before opening). The companion must be a regular file physically in
  the real binary's directory.
- A symlinked companion is a **loud startup error**, never a silent
  skip — someone put it there. This includes a *dangling* symlink:
  `Stat` (which follows links) sees nothing, so pinned candidates get
  an `Lstat` cross-check that catches the squatter before it becomes a
  live redirect.
- `/etc` and the XDG location are deliberately not pinned
  (symlink-overlay distros like OpenWrt), and `--config` is exempt: an
  explicit user path is the user's business.

Files are transcoded by extension via format providers; JSON is handled
natively. An explicit `--config` file whose extension no registered
provider handles is a **startup error**. The location search, by
construction, only probes the extensions it knows (`json` + every
registered provider's): a config file at a standard location with an
unhandled extension is simply outside its view and silently unused —
the search never enumerates directories, so foreign files
(`config.json.bak`, package-manager droppings) can never break
startup. More than one existing candidate at the *same* location
(e.g. both `config.json` and `config.yaml`) is ambiguous — a startup
error. Trailing data after a config file's top-level JSON object is a
startup error too, never silently dropped.

A config source must resolve to a **regular file**: the `stat` probe —
which follows symlinks, so a symlink to a regular file still passes
(symlink-overlay distros keep working) — refuses FIFOs before any open
could block on them, and gives devices and directories a clean startup
error. The pinned companion location additionally forbids the symlink
hop itself.

Config files are also size-capped: a file larger than the cap (default
1 MiB, which covers any sane configuration) is refused with a loud
startup error — the size is checked on the same `stat`, **before the
file is even opened**; an oversized config is never opened, read or
parsed. A capped
reader underneath is defense in depth against stat races and lying
sizes, and never truncates silently. Like feature suppression the cap
is a build-time property of the binary: `fw.MaxConfigSize(bytes)`
before `Main`.

The core's `--config,-c` path is itself an ordinary config value with the
usual source precedence — default empty, settable via env, the argument
always wins. When it resolves non-empty, that single file **is** the
configuration and the three-location search is skipped entirely; when
empty, the locations are searched and merged as above. One deliberate
exception: in `--write-config` mode a *missing* target does not replace
the search — the locations are loaded as usual so the written file
reflects the currently effective configuration, and the target is only
created (an *existing* target is loaded as the explicit config, per the
input-and-output rule in §5).

The run-scoped core values are locked down (`dump:"-"`, `env:"-"`):

- `help` and `writeConfig` are **argument-only** — a config file or an
  inherited environment variable setting them would be a persistent
  denial (every run printing help, or writing a config and exiting).
- `config` is settable by argument or environment (`APPLETID_CONFIG`
  is a legitimate deployment pattern; the pointed-at file still passes
  every gate), but never by a config file.
- All three are excluded from `--write-config` output, and a config
  file attempting to set any of them is a loud startup error.

The `dump:"-"` tag is available to any service's config struct, with
the same run-scoped semantics: excluded from `--write-config` output
and refused from config files.

### Core feature suppression

A binary may remove pieces of the core's configuration surface — a
hardened or embedded deployment should be able to forbid config
redirection and service rewiring:

```go
func main() {
    fw.Suppress(fw.FeatureConfigFile, fw.FeatureOverride)
    fw.Main()
}
```

Suppressible features: `FeatureConfigFile` (`--config,-c`),
`FeatureWriteConfig`, `FeatureDisable`, `FeatureEnable`,
`FeatureOverride`, `FeatureHelp` (`--help,-h`; help and write-config
are argument-only, so suppressing them closes their single door). A suppressed feature vanishes from the core schema
entirely: its argument becomes unknown (strict-pass error), its env var
is never consulted, and its key appearing in a config file's `core`
section is a **loud startup error** — operators learn it is not honored
instead of wondering why it is ignored. Suppression is a build-time
property of the binary (called from `main()`/`init()` before `Main`),
not runtime configuration.

Noted interaction: with sinks opt-in, a binary that suppresses
`FeatureEnable` leaves the operator no path to any linked sink — the
logging floor is all they get unless the developer wires a sink in as
a code-level dependency. That is the point of Suppress (the builder's
deliberate lockdown), stated here so nobody discovers it in
production.

Misusing the build-time API is itself a collected startup violation,
never silently ignored: `MaxConfigSize` with a non-positive limit,
`Suppress` of the default-off `FeatureSCMDebug`, `Enable` of a
default-on feature, and unknown features everywhere.

### Format providers

```go
type ConfigFormatProvider interface {
    Extensions() []string                       // e.g. ["yaml", "yml"]
    ToJSON(in io.Reader) (io.Reader, error)     // native format → JSON
    FromJSON(in io.Reader) (io.Reader, error)   // JSON → native format (--write-config)
}
```

Documented contract: `ToJSON`/`FromJSON` are **pure stream transforms**,
usable before anything is configured or started (the core needs them
during step 4, pre-lifecycle). A provider claiming the native `json`
extension, or an extension another provider already claims, is a
startup violation. Providers are ordinary services: registered
cold, discovered by interface, used statelessly. The provider whose
extension matched an actually loaded file (or the `--write-config`
target) is added as a closure seed — it receives the normal lifecycle and
survives ejection, keeping a future value-only config reload able to
re-read the file. Unused providers stay cold and are ejected. A provider
wanting an unconditional lifecycle declares a dependency or is forced in
with `--enable`.

### Argument syntax

- `--long value`, `--long=value`, `-s value`, `-s=value`.
- Bools: bare presence = true; `=false` to unset.
- Slices: flag repetition appends (`--tag a --tag b`); env values
  comma-separated, with an empty env value meaning an empty slice (the
  only way to express one from the environment); JSON arrays in files.
  Precedence made concrete: the **first** argument occurrence of a slice
  flag replaces any file/env-sourced content, repetitions append.
- Short-flag bundling: `-abc` — every bundled flag must be a bool except
  the last, which may take a value (`-abc=5`, `-abc 5`).
- A literal `--` ends flag parsing; everything after it is positional.
- **Positionals:** every bare token after the last flag argument is
  collected as a positional and does not cause errors; a bare token
  *followed by* another flag is a strict-parse error ("positionals must
  come last"). Parsing/routing of positionals is deferred — v1 collects
  them and exposes them via `fw.Positionals()`, nothing more.
- Durations are strict in every source: a unit suffix is required
  (`5s`, `5000ms`, `5000000ns`; bare numbers are rejected), and in JSON
  files a duration must be a *string* — never a number.
- Name lexicon: long names are lowercase, at least two characters,
  letter-first, letters/digits/dashes, no trailing dash; short forms are
  one ascii letter/digit; env names are uppercase letters, digits and
  underscores, not digit-first. Embedded fields in config structs are
  not supported (registration error).
- Duplicate explicit env names across the closure are a startup error
  (derived names cannot collide because long names are unique).

## 7. Logging & Tr()

### Logging

Built on `log/slog`. A log sink is a service declaring
`Provides[slog.Handler]()` — console, file, syslog/journald ship as
subpackages, each with its own config struct. Sink activation falls out of
the normal machinery (imports, closure, enable/disable): a sink is used
when it is `--enable`d or pulled by a genuine dependency, and stays cold
otherwise. No sink is on by default — the framework guarantees a raw
stderr floor instead (below), and the console sink is opt-in like every
other.

The core assembles a **multihandler** over every enabled sink:
`Enabled` = any child accepts; `Handle` fans out to accepting children
(child errors are joined, one failing sink never blocks the rest);
`WithAttrs`/`WithGroup` derive views of all children.

**Sink-author contract** (standard slog semantics, stated explicitly):

- `WithAttrs`/`WithGroup` must return derived *views* sharing the
  underlying output resource — never `return s` (loses the attrs), never
  a deep copy (duplicates the resource). Views are ephemeral values;
  only the registered service instance owns the resource and has a
  lifecycle.
- Handlers must be safe for concurrent use; many derived loggers share
  one sink.
- `Handle` must be prompt and apply its own I/O deadlines (a network
  sink sets write timeouts on its connection). The multihandler is
  deliberately synchronous — fan-out happens on the caller's goroutine;
  a hung sink is the sink's bug, not the core's to babysit. Known
  limitation: the shipped syslog sink is stdlib `log/syslog`-based,
  which exposes no deadlines — its default local socket is unaffected,
  but remote `tcp` cannot fully honor this contract; anyone needing
  deadline-guaranteed remote logging is better served by the future
  async decorator (Open Items), which bounds any slow sink.
- A sink SHOULD be **fully operational when `Configured()` returns** —
  it acquires its own resources there (the file sink opens its file in
  Configured, not Start), so it is live for the buffer replay at the
  swap and captures the complete startup history. `Start` is typically a
  no-op (sinks stay `Starter`s because only started Starters receive
  `Stop`, which is where resources close). A sink that *cannot* be
  operational until its own `Start` — e.g. a DB logger depending on a
  started pool service — is legal but late-joining: its inert guards
  (`Enabled` false while unready) make it invisible to the replay and to
  early records; it joins the stream from its `Start` onward. Records
  before that exist only in the sinks that were ready.

Bootstrap: the core's initial `slog.Default()` is a **buffering handler**
collecting every record emitted during startup. After the `Configured`
phase, the multihandler is assembled, the buffer **replays** into it, and
the default swaps over.

- Zero `slog.Handler` services in the closure → the core's unconditional
  **logging floor**: a plain stderr text handler. This is the default —
  a binary that links no sink, or links sinks but enables none, still
  gets startup and runtime records on stderr. Enabling a sink
  (`--enable console`, or a dependency) replaces the floor with that
  sink's configurable output.
- There is **no silence switch**. The floor is always present; a binary
  that wants no output redirects its stderr (`2>/dev/null`) — the shell
  already does this, the framework does not reinvent it. Use
  `--console-level error` (with console enabled) for quiet-not-mute.
- Startup failure before the swap → the buffer flushes to stderr so
  diagnostics are never swallowed.

Services log via `slog` normally; no injected logger is needed (though a
service may `inject:""` a specific `slog.Handler` for direct access).

### Tr()

```go
func Tr(format string, args ...any) string
// Tr("valueA: {int} and valueB: {bool}", "bool", false, "int", 100)
//   → "valueA: 100 and valueB: false"
```

- `args` are name/value pairs; `{name}` placeholders resolve by name with
  `%v` formatting semantics.
- `{{` and `}}` escape literal braces.
- A placeholder with no matching name — or a malformed pair (non-string
  name, trailing odd value) — is left verbatim (visible, harmless).

**gettext is the committed i18n model** (translate-then-format, the
classic convention): the untranslated format string is the msgid,
translation providers will load `.po`/`.mo` catalogs and look the format
up before substitution, and locale selection follows gettext conventions
(`LANGUAGE`/`LC_ALL`/`LANG`). The `{name}` placeholder syntax matches
gettext's `python-brace-format` flag, so standard tooling (`msgfmt
--check`, Poedit, Weblate) validates placeholders in translations.
`usage:` strings join the same `.pot` extraction set.

### The Translator seam

```go
type Translator interface {
    Translate(msgid string) (translated string, ok bool)
    TranslateN(msgid, msgidPlural string, n int) (translated string, ok bool)
}

func TrN(msgid, msgidPlural string, n int, args ...any) string
```

The core itself depends on the translator — it is a core facility
discovered through the registry, not an applet dependency:

- A service declares `Provides[Translator]`. **Exactly one** may be
  registered — more than one is a startup violation (a developer
  error: two catalog systems linked into one binary).
- If present, it is an **optional dependency edge of the core node**
  (§5) and thereby joins every closure. `--disable` still wins: the
  operator can force raw msgids, exactly as `--disable` drops any
  other service.
- **Configured-first, everywhere output renders**: the translator's
  dependency subtree gets Inject + `Configured` before anything is
  printed — first in the ordering on normal runs, and on both
  short-circuit paths (`--help`, `--write-config`), which otherwise
  run no lifecycle at all. Full config citizenship follows: locale
  overrides and catalog knobs are ordinary config fields with args/
  env/file precedence, visible in help, written by write-config.
- The contract mirrors the sink contract: **operational when
  Configured returns**. `Start`/`Stop` are not part of a Translator's
  job.
- **Failure is quiet degradation**: if the translator's `Configured`
  fails, the core logs one buffered warning and proceeds untranslated
  — translations never fail a startup and never change an exit code.
  (The multiplicity violation stays fatal; that one is a developer
  mistake, not a locale problem.)
- Lookup is translate-then-format: `Tr` looks the raw format string
  up as the msgid and runs the placeholder scan over the translation;
  a translation with wrong placeholders degrades to verbatim braces,
  never breaks.
- `TrN` is the plural authoring surface, present from day one so no
  message is ever written singular-only (retrofitting plurals means
  touching every call site and invalidating catalogs). gettext
  parameter order; `{n}` is implicitly bound to the quantity (a
  caller-supplied "n" pair is shadowed; documented as reserved); on a
  missing translator, a `TranslateN` miss, or degradation the English
  rule picks between the msgids (`n != 1`) — msgids are
  English-shaped by doctrine, so the fallback is always coherent. The
  catalog's `Plural-Forms` formula (evaluated by the i18n module, not
  the core) picks the form otherwise — Bulgarian gets its count form,
  Russian its three forms, Arabic its six.

Documented edges: dispatch-failure usage renders msgids (no applet id
yet — nothing to fill or configure); `Introspector.Arguments` stays
zero-side-effect and never runs `Configured`, so completion
descriptions use whatever translator state exists at query time.

Catalog loading (`.mo` parsing, locale-chain selection, embedded-FS
handoff, the `Plural-Forms` expression evaluator) is the separate
`sxcli.dev/i18n` module — the ecosystem pattern, again: the core owns
the seam, the module owns gettext.

## 8. Tooling — sxcli.dev/vet

The reflection-and-tags bet is only defensible with tooling that
catches its failure modes before runtime; this is the price of the
bet, paid explicitly. A separate module — **`sxcli.dev/vet`**, binary
`sxclivet`, built on `golang.org/x/tools/go/analysis` — runs standalone
and as `go vet -vettool=`; the `x/tools` dependency never touches the
core's go.mod. The composition model is what makes deep static
analysis tractable: ids are exported constants, `Accept`/`Order`
chains are source-visible in `main`, and the generic `Register[T]`
hands the analyzer the concrete type through `go/types` without
constructing anything.

Checks:

- **exported-id** — every `Register` id is (or matches) an exported
  package-level constant (`ID` / `XxxID`); raw string literals and
  registering packages exporting no constant are flagged. The constant
  is the package's public handle; without it consumers are back to
  magic strings.
- **identity** — the id constant **begins with the package's import
  path** (the uniqueness guarantee the runtime cannot check); every
  registration chain **ends in `.Register()` or is consumed by
  `Solo`** (the forgotten-terminal hole, closed statically) and
  reaches `.Alias(...)` before its terminal; literal aliases are valid
  (lowercase, digits, hyphens) and inject-by-id tags reference known
  ids.
- **composition** — `Accept`/`Order` ids exist in the statically
  reconstructed catalog; `Order ⊆ Accept`; duplicates.
- **graph viability** — the static mirror of `Build()`: required
  dependencies of the accepted set resolvable (`types.Implements` in
  place of reflection), unbroken ambiguity flagged with both
  candidates named.
- **unaccepted services** — a package whose cataloged services are
  *all* unaccepted (in the new world this is almost definitionally a
  blank import). **Warning by default, promoted to error by
  `-strict`**: partial acceptance is the feature working as designed
  and stays silent; total non-acceptance is legal mid-development and
  a leash in CI.
- **tag grammar** — misspelled tag keys, malformed `inject` grammar,
  the whole uncheckable-string-DSL complaint, checked. Pays rent in
  every package, composed or not.
- **derived-name collisions** — the env namespace's underscore joins
  make distinct field paths collidable, and explicit `env:` names are
  global claims: every collision `Build()` would report at runtime is
  reported at compile time, across the whole composition.

Stated limits: non-constant ids and dynamically built option slices
produce a "cannot verify" diagnostic, never silence. Drift between the
analyzer's matching semantics and the graph's real ones is the design
risk; the mitigation is a shared **conformance corpus** — fixtures
that fw's `Build()` tests and the analyzer's tests must judge
identically (code cannot cross the internal boundary; verdicts can).
The runtime 90% needs no tooling at all: `Build()` returns an error,
so a one-line `TestComposition` asserts the whole graph in CI.
`sxclivet`'s added value is IDE/CI feedback before any test runs, and
the checks tests cannot express.

## 9. Testing Strategy

- **Registry isolation:** public `Register` delegates to a default registry;
  internals construct private registries so `init()` side effects never
  contaminate tests.
- **Hermetic pipeline tests:** the pipeline takes its inputs (arg vector,
  env lookup, config search paths, platform hooks) through small internal
  seams. Full lifecycle tests register fake services/applets in a private
  registry and drive the pipeline with synthetic args/env/config files in a
  temp dir, asserting call order, injected fields, merged config, and exit
  codes.
- **Unit coverage per risk pocket:** graph (topo order, cycles,
  disable/enable/override), arg parser (bundling, `=` forms, positionals,
  duplicates), tag reflection (all supported types + error cases), config
  merge precedence, multihandler fan-out, buffer/replay, `Tr` (pairs,
  escapes, missing names).
- **Windows:** the platform layer is an interface, so SCM pipeline logic is
  testable anywhere; Windows-specific tests run under **Wine** using
  `x/sys/windows/svc/debug`.

## 10. Open Items (deferred by decision)

| Item | State |
| --- | --- |
| `ConfigurationUpdated` trigger (file watch? signal? API?) | interface reserved, semantics open — but constrained: a reload only re-fills config values of closure members; the graph is immutable once resolved (no add/remove/rewire, ever) |
| Terminal UI provider | concept named, comes after v1 |
| i18n catalog module (gettext `.po`/`.mo` loading, locale chain, `Plural-Forms` evaluator, embedded-FS handoff) | the core seam (Translator, TrN, seeding, Configured-first) is DONE — see §7; the catalog implementation is the separate `sxcli.dev/i18n` module, next in line |
| Demo applet | undecided; will not mirror busybox applets |
| Positional parsing/routing | positionals collected, routing open |
| `inject` optional-with-IDs interactions beyond v1 needs | extend grammar as needed |
| Custom value parsers (e.g. `type UnixTime` with a user-provided parser service, discovered like format providers) | deliberately not in v1 — the converter is a single switch; a parser registry slots in front of it when someone actually needs one |
| Embedded configs in the binary (e.g. a `go:embed`-ed default config compiled into the consumer's binary, lowest-priority file source before the on-disk locations) | future version; slots into the existing merge order as a pre-location source and needs no new precedence rules |
| Async log sink decorator (bounded queue + writer goroutine wrapping any `slog.Handler`, drop-counting on overflow, flush on Stop) | deliberately not in v1 — the multihandler stays synchronous; decoupling is an opt-in wrapper service if the need materializes |
| Showing defaults alongside effective values in `--help` (`value: X (default: Y)`) | deferred — needs a pre-merge snapshot in NewSchema (~20 lines, no API change); add when it earns its keep |
| A core argument listing all registered applets (e.g. `--applets`) | future improvement — today the applet list only appears in dispatch-failure usage output |
| Refusing to load group/world-**writable** configs (the injection vector — the read-side sibling of the pinned-location hardening; what sudoers/sshd refuse) | to be designed deliberately: unix-only, `/etc` + companion locations, XDG exempt (user-owned by definition), Windows ACLs out of scope |
| Logical/boolean `inject` expressions for single-valued fields (e.g. `inject:"mysql \|\| sqlite"` — a preference list letting the service express fallbacks without forcing user overrides) | future syntax extension; must compose with `override` remapping (each alternative remapped before resolution); until then a non-slice field names at most one id |
| Positional declarations for introspection/completion | still open; field-level self-description landed as the chain's `Metadata` |
| Shell completion service | decided: a SEPARATE module (`sxcli.dev` namespace), never in core — the first external Introspector consumer; registers per-shell `System` applets (invoked `binary <id> …` by the generated scripts); any capability gap it hits is fixed as a core API improvement, never a backdoor |
| Disabling first-token applet dispatch entirely (build-time policy for binaries that want basename/single-applet behavior only) | idea noted while designing Hidden/System — registration/`Suppress`-style knob, unscheduled; interaction with System selectors must be resolved when designed |
| Composition release fallout | §4's model, implemented: fw rework (catalog, Builder, identity/alias split, registration chain), every ecosystem package gains a path-ID constant, a declared alias and the factory registration shape (completion shells, sinks, yaml, future i18n), docs/site/README rewritten; ships as one breaking release together with the four committed rework phases (AlwaysOn removal, core node, subtree, exactly-once) |
| Package-level `Suppress`/`Enable`/`MaxConfigSize` under the Builder | the globals read like leftovers once the Builder exists (`.Suppress(…)` as a chain method is the obvious home); undecided, decide during composition implementation |
| `fwtest` public test harness | unblocked by `Build() (App, error)` — compose, build, run, assert; the internal world harness made public |
| `sxcli.dev/conf` extraction (the config engine as a standalone module) | STAGED: the engine now lives at `sxcli.dev/fw/conf` (public package, moved out of internal 2026-07-18 after the Section decoupling left it importing only internal/fail); the module split at the v1 horizon becomes an import-path rename; the standalone front door (`conf.Load`, no Collector in its signature) arrives with the pipeline promotion |
| Config schema versioning & migration chain | DESIGNED 2026-07-18 (§6): mandated `Version uint32` + typed per-section `conf.Step` chain, version-implies-complete, migrate-then-merge; supersedes the earlier "renamed from" metadata idea; lands with the conf pipeline promotion |
