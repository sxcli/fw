# sxcli-fw — Design Specification

sxcli stands for **Simple Extensible CLI**.

Date: 2026-07-09
Status: approved section by section during brainstorming
Module: `github.com/sxcli/sxcli-fw` · Package: `sxclifw` · Go: 1.26

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
├── go.mod                — module github.com/sxcli/sxcli-fw
├── *.go                  — package sxclifw: the entire public API
├── platform_unix.go      — args acquisition, no-op service hooks
├── platform_windows.go   — SCM integration (svc.Run, handler, SCMApplet)
├── internal/
│   ├── registry/         — service descriptors, registration validation
│   ├── graph/            — closure resolution, topological order
│   ├── config/           — file discovery, source merging, arg/env parsing
│   └── platform/         — platform layer internals if they outgrow root files
├── sink/
│   ├── console/          — slog.Handler → terminal (registers as AlwaysOn)
│   ├── file/             — slog.Handler → file
│   └── syslog/           — slog.Handler → syslog/journald
└── configfmt/
    └── yaml/             — YAML ⇄ JSON config format provider
```

Consumers choose what links into their binary via blank imports:

```go
import (
    fw "github.com/sxcli/sxcli-fw"
    _ "github.com/sxcli/sxcli-fw/sink/console"
    _ "github.com/sxcli/sxcli-fw/configfmt/yaml"
)

func main() { fw.Main() }
```

Providers only import the root package — no import cycles. Everything under
`internal/` is invisible; the root package is the entire public contract.

## 3. Service Model — Interfaces

All in package `sxclifw` (Go-conventional `-er` names):

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

// AlwaysOn services are active regardless of the applet's dependency
// closure. Embeds Starter, guaranteeing a full lifecycle. Structurally
// identical to Starter — always-on status comes from the explicit
// Provides[AlwaysOn]() declaration at registration (no marker method, so
// third-party packages can implement it).
//
// WARNING: an AlwaysOn service is configured, started, and stopped for
// EVERY applet in the binary, whether that applet needs it or not. It
// taxes every invocation with its startup cost, its configuration
// surface, and its failure modes. It SHOULD NOT be used lightly, if at
// all — almost every service belongs in the normal dependency closure
// instead. AlwaysOn exists for framework-level infrastructure (log
// sinks) and little else. The framework reserves the right to disable
// or remove AlwaysOn support in a future version; do not build designs
// that depend on it.
type AlwaysOn interface {
    Starter
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

- A service registered with `Provides[AlwaysOn]()` receives
  Configured/Start/Stop on every invocation, in every closure.
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

## 4. Registration & Dependency Injection

### Registration API

```go
func Register(id string, instance any, opts ...RegisterOption)
func Provides[I any]() RegisterOption          // declares a provided interface
func WithConfig(cfgPtr any) RegisterOption     // pointer to the Configuration struct;
                                               // its field values are the defaults
```

Called from package `init()`; one package may register many services. The
public `Register` delegates to a package-level default registry (tests build
private registries).

At registration the core builds a **descriptor**: service ID, concrete type,
declared interfaces (each verified by reflection against the instance),
config schema (reflected from the config struct's tags), and dependency
fields. Registration **never panics** — every violation is recorded and
startup fails reporting *all* problems at once:

- invalid ID (must be lowercase, Go-identifier style; `core` is reserved),
- duplicate ID,
- the same concrete struct type registered more than once (forbidden),
- instance does not implement a declared interface,
- applet implementing Starter/Stopper,
- malformed tags / unsupported config field types,
- `inject` tag on an unexported field,
- slice-of-concrete-struct `inject` field.

The concrete struct type is recorded automatically — no option needed —
so dependents may require the service by `*Struct` or by any declared
interface.

After `Register` the instance belongs to the framework: register a
literal (`Register("x", &X{}, …)`) and keep no references. Once the
closure is resolved, services outside it are **ejected** from the
registry so their instances can be garbage collected before `Configured`
ever runs (best effort — a kept package-level reference defeats it).

### Dependency declaration

A struct tag on exported fields of the registered *instance*:

```go
type MyService struct {
    Log     slog.Handler    `inject:""`            // by interface, first registered
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
- Single-valued field → the first registered match (or the named ID).
- Slice fields: **interface element types only**. Injection delivers *all
  enabled* matching services in registration order — with listed IDs the
  slice may contain *more* than listed (always-on services and other
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
2. **Single-applet mode:** if exactly one applet is registered, it is
   always dispatched. argv[0] is ignored and *no applet-selector
   consumption happens at all* — the entire argument vector (after the
   binary name) belongs to the applet as ordinary flags/positionals. This
   lets the framework serve simple applications with no thought given to
   binary names, symlinks, or subcommands. Notes:
   - `binary myapplet --args` does **not** treat `myapplet` as a selector
     even when it matches the sole applet's ID — it is a leading bare
     token under normal arg parsing. Selector logic is fully off; no
     "data or selector?" ambiguity.
   - Registering a second applet re-enables selector logic (rules 3–4),
     changing the binary's command-line contract. That breaking change is
     the developer's responsibility to manage.
   - Only dispatch changes. The sole applet's ID still anchors the
     `APPLETID_` env prefix and config file names; closure resolution,
     disable/enable/override, and the lifecycle proceed as usual.
3. **If the first argument exists and does not start with `-`:** it is
   always an applet selector. Look it up, dispatch with the remaining args,
   overriding argv[0]. Unknown name → dispatch failure — even if
   basename(argv[0]) is itself a valid applet. No fallback.
4. **Otherwise:** basename(argv[0]) must name a registered applet; dispatch
   with all args.
5. Every dispatch failure (including a binary with zero registered applets)
   prints usage — including the list of registered applet IDs — to stderr
   and exits non-zero. In single-applet mode the applet list is dropped
   from the usage output. `--help` renders only the dispatched applet's
   argument schema (core + closure, grouped by service ID) and never an
   applet list; enumerating applets is a future core argument (see Open
   Items).

Consequence (documented): in multi-applet binaries a leading bare token is
*never* applet data. Scripts must know `binary appletName --args`
dispatches to `appletName`, and a symlinked applet cannot take a bare first
positional.

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
  5. closure resolution: applet + AlwaysOn + transitive deps,
     with disable/enable/override applied; dependency-ordered via SCC
     condensation (cycles are warnings, registration order within);
     cold services are ejected from the registry so their instances
     can be garbage collected
  6. strict full parse — complete arg/env schema now known
     (core + every closure member); unknown argument = error
  7. fill each closure member's config struct (in place, merged values)
  8. inject dependency fields
  9. Configured() on each Configurable, dependency order
 10. assemble log multihandler, replay startup buffer, swap slog default
 11. Start() on each Starter, dependency order, sequential
 12. code = applet.Run()          (SCM mode: applet.Execute(...))
 13. Stop() on each started Starter, reverse start order
 14. os.Exit(code)
```

`--write-config` short-circuits after step 7's merge: write the merged
config to the `--config` target (format chosen by the file extension via a
format provider) or, with no target, dump JSON to stdout; exit 0 without
Configured/Start/Run. The target is input *and* output: an existing
target is loaded as the explicit config first — making `--write-config`
an easy way to normalize/reformat an existing file — and a missing one
is only created. Written files get mode 0600.

`--help,-h` (core-owned) prints the dispatched applet's full argument schema
— core + entire closure, grouped by service ID, with usage texts (rendered
through `Tr()`) and the **current effective values** (all sources already
merged: what the binary would actually use) — to stdout and exits 0.

### Config-driven service control

Part of the core's config struct (settable via args, env, or file like
everything else):

- `disable` — service IDs removed from the closure even if required.
- `enable` — service IDs forced into the closure (with their transitive
  dependencies) even if nothing requires them.
- `override` — ID remapping (`sqlite=mysql`): wherever a dependency names
  `sqlite`, resolve `mysql` instead. The substitute must satisfy the
  dependency's field type (checked at resolve time) or startup fails.
  A disabled required dependency without a substitute is a startup error.

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

Registered via `WithConfig(ptr)`; field values at registration are the
defaults; the core fills the same struct in place before `Configured()`.

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
- `arg:"long[,short]"` — explicit opt-in per field; no tag → no CLI arg and
  no env var (file-only). Duplicate long names across the closure = startup
  error; short names are first-come-first-served.
- `env:"NAME"` — explicit env name; if absent but `arg` present, derived as
  `APPLETID_` + long name uppercased with dashes → underscores
  (applet `cat`, arg `log-max-age` → `CAT_LOG_MAX_AGE`).
- `usage:"…"` — help text; rendered through `Tr()`; doubles as a gettext
  extraction source when translation support lands.

Supported field types (v1): `string`, `bool`, all int/uint widths, floats,
`time.Duration`, and slices of these. Nested structs are allowed for
file/JSON structure but their fields are file-only (no `arg`/`env` tags) in
v1. Anything else (maps, custom types) is a registration error.

### Sources & precedence (least → most important)

```
defaults  <  config files  <  environment  <  arguments
```

Config files, loaded in order, later overriding earlier field-by-field:

1. `<binary-dir>/<applet-id>-config.<ext>` (next to the real binary)
2. `/etc/<applet-id>/config.<ext>` — or the Windows equivalent
3. XDG user config location: `<xdg-config>/<applet-id>/config.<ext>` — or
   the Windows equivalent

(Exact Windows locations to be pinned down during implementation of the
platform layer.)

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
  skip — someone put it there.
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
startup.

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
empty, the locations are searched and merged as above.

The run-scoped core values are locked down (`dump:"-"`, `env:"-"`):

- `help` and `writeConfig` are **argument-only** — a config file or an
  inherited environment variable setting them would be a persistent
  denial (every run printing help, or writing a config and exiting).
- `config` is settable by argument or environment (`APPLETID_CONFIG`
  is a legitimate deployment pattern; the pointed-at file still passes
  every gate), but never by a config file.
- All three are excluded from `--write-config` output, and a config
  file attempting to set any of them is a loud startup error.

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
wanting an unconditional lifecycle may still declare `Provides[AlwaysOn]()`
itself.

### Argument syntax

- `--long value`, `--long=value`, `-s value`, `-s=value`.
- Bools: bare presence = true; `=false` to unset.
- Slices: flag repetition appends (`--tag a --tag b`); env values
  comma-separated, with an empty env value meaning an empty slice (the
  only way to express one from the environment); JSON arrays in files.
- Short-flag bundling: `-abc` — every bundled flag must be a bool except
  the last, which may take a value (`-abc=5`, `-abc 5`).
- **Positionals:** every bare token after the last flag argument is
  collected as a positional and does not cause errors. Parsing/routing of
  positionals is deferred — v1 collects them and exposes them via
  `sxclifw.Positionals()`, nothing more.

## 7. Logging & Tr()

### Logging

Built on `log/slog`. A log sink is a service declaring
`Provides[slog.Handler]()` — console, file, syslog/journald ship as
subpackages, each with its own config struct. Sink activation falls out of
the normal machinery (imports, closure, enable/disable); the console sink
registers itself as `AlwaysOn` so there is sane output by default.

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
  syslog sink sets write timeouts on its connection). The multihandler
  is deliberately synchronous — fan-out happens on the caller's
  goroutine; a hung sink is the sink's bug, not the core's to babysit.
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

- Zero enabled `slog.Handler` services after resolution → the core
  force-pulls the console sink into the closure — **unless the operator
  explicitly disabled it**: disabling every sink means deliberate
  silence, respected as stated ("your choice, your problem"). Use
  `--console-level error` for quiet-not-mute.
- Console sink not even registered (package never imported) → last resort
  is a plain stderr text handler.
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
`usage:` strings join the same `.pot` extraction set. v1 ships the
identity lookup — pure formatting. Plural support (`TrN`, gettext
`ngettext`/`Plural-Forms`) is a future addition.

## 8. Testing Strategy

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

## 9. Open Items (deferred by decision)

| Item | State |
| --- | --- |
| `ConfigurationUpdated` trigger (file watch? signal? API?) | interface reserved, semantics open — but constrained: a reload only re-fills config values of closure members; the graph is immutable once resolved (no add/remove/rewire, ever) |
| Terminal UI provider | concept named, comes after v1 |
| i18n providers (gettext `.po`/`.mo` catalog loading, locale selection) | gettext is the decided model; `Tr()`/`usage` are the extraction sources, format strings are msgids |
| `TrN` plural support (gettext `ngettext`/`Plural-Forms`) | future addition alongside catalog providers |
| Demo applet | undecided; will not mirror busybox applets |
| Positional parsing/routing | positionals collected, routing open |
| `inject` optional-with-IDs interactions beyond v1 needs | extend grammar as needed |
| Custom value parsers (e.g. `type UnixTime` with a user-provided parser service, discovered like format providers) | deliberately not in v1 — the converter is a single switch; a parser registry slots in front of it when someone actually needs one |
| Embedded configs in the binary (e.g. a `go:embed`-ed default config compiled into the consumer's binary, lowest-priority file source before the on-disk locations) | future version; slots into the existing merge order as a pre-location source and needs no new precedence rules |
| Async log sink decorator (bounded queue + writer goroutine wrapping any `slog.Handler`, drop-counting on overflow, flush on Stop) | deliberately not in v1 — the multihandler stays synchronous; decoupling is an opt-in wrapper service if the need materializes |
| Showing defaults alongside effective values in `--help` (`value: X (default: Y)`) | deferred — needs a pre-merge snapshot in NewSchema (~20 lines, no API change); add when it earns its keep |
| A core argument listing all registered applets (e.g. `--applets`) | future improvement — today the applet list only appears in dispatch-failure usage output |
