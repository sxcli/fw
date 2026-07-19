# Changelog

## v0.3.0 — the composition release

This release breaks everything, on purpose, once. The trigger was small
and damning: **sorting your imports changed your program's behavior.**
Services used to register through blank imports, dependency ties went to
the first registration, and registration order was import order — so
`goimports` reordering a block could silently rewire which service
satisfied an interface. A framework built on "never silent" could not
stand on that.

### What replaced it

- **Composition is explicit.** `init()` only *catalogs* a service;
  a binary names what it takes: `fw.Builder().Accept(pkg.ID, …).Main()`,
  or `fw.Solo(...)` / `fw.Main()` to accept everything. Blank imports
  are dead — packages export `ID` constants, and importing one is
  justified by naming it.
- **Ties are never broken silently.** Ambiguous dependencies are
  startup errors; rank candidates with `Order(...)` or name an id in
  the inject tag. First-registered-wins is gone.
- **Identity split.** A service has an *id* (import-path-shaped, for
  code) and required *aliases* (short names, for operators — config
  sections, env prefixes, selectors, `--disable`).
- **The package is `fw` now** (#1): `import "sxcli.dev/fw"`, no alias
  needed.
- **Registration is a chain**:
  `fw.NewRegistration(ID, factory, accessor).Alias("name").Register()`.
- **The `arg:` tag died; `conf:"long,short"` is the one operator
  name** — it feeds both the flag and the derived env var. Derived env
  names are path-stitched (`ALIAS__FIELD`, `ALIAS__SECTION__PATH`).
- **Config schemas are versioned.** Every config struct declares
  `Version uint32`; schemas evolve through typed migration chains
  (`.Migrate(fw.Step(1, func(old V1) V2 {…}))`), and deployed files
  keep working. `--upgrade-config` modernizes a file in place;
  `--validate-config` checks everything and exits; `--help` is
  best-effort and survives a broken config.
- **Positionals are declared** (`pos:"0"`, `pos:"rest"`) — validated,
  in `--help`, applet-owned. `fw.Positionals()` is gone.
- **The config engine moved out**: it is now
  [`sxcli.dev/conf`](https://sxcli.dev/conf) — usable standalone as a
  cobra/viper replacement; fw is its first consumer.
- **Static analysis exists**: [`sxcli.dev/vet`](https://sxcli.dev/vet)
  (`sxcli-vet`) catches bad ids, missing terminals, tag mistakes,
  broken migration chains and ambiguous compositions at compile time —
  including everything this release's error messages promise it
  catches.

Companion releases: `sxcli.dev/conf v0.1.0`, `sxcli.dev/completion
v0.2.0`, `sxcli.dev/vet v0.1.0`.

## v0.2.0

Additive: `SingleApplet()`, `HintServiceID`, `Hidden()`/`System()`
registration options and hint plumbing — the core prerequisites of the
`sxcli.dev/completion` module.

## v0.1.0

First public release: the service model, dependency closure, the
configuration pipeline (args/env/files with strict validation),
`log/slog` sinks, translation seam, Windows SCM support, busybox-style
multi-applet dispatch.
