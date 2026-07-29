# Adversarial conformance review — the system service + core-arguments-argv-only

Scope: `v0.3.0-to-v0.4.0-the-system-service.md` and
`v0.3.0-to-v0.4.0-core-arguments-argv-only.md`, both STATUS=IMPLEMENTED.
Repos: sxcli-fw, sxcli-conf, sxcli-completion (rules/tags for the fence).
Baseline: `go test ./...` green in fw and completion.

## Verdict

Both invariants hold at the mechanism level. Forgery, squatting and the
env→closure/env→config/env→serving vectors are closed on the code paths
the docs name. The core-contribution fence and the argv-only tags are
correct and structurally enforced. TWO defects, both in the
"environment-blind" claim's completeness, neither a code hole in the
enforced surface: one residual env→closure vector the docs declare
impossible (D1), one stale security comment describing a closed hole as
open (D2). Everything else investigated cleared.

## Defects

### D1 — the introspection view's env-blindness is partial: config-file LOCATION is still env-derived (Medium)

`systemservice.go:40-44` builds the env-blind view by copying the runtime
and zeroing **only** `lookupEnv`:

    envless := *s.rt
    envless.lookupEnv = func(string) (string, bool) { return "", false }

`envless.locations` is untouched — it stays `engine.ProductionLocations`,
whose user tier calls `os.UserConfigDir()` → reads `$XDG_CONFIG_HOME`
(then `$HOME`) at `sxcli-conf/engine/sources.go:61`. So the "env-blind"
view still consults the ambient environment to decide WHERE config files
live, and a config file legally carries `core.enable`/`core.disable`
(argv-only doc, permitted). The environment therefore still reshapes the
closure the introspection/completion view advertises — precisely the
env→closure vector both docs declare closed:

- argv-only doc L84: "the interactive shell's environment is never read
  at all."
- system-service doc L10 / argv-only L8-11: "environment-blind by
  construction … every facility inherits this."

Both claims are false: `$XDG_CONFIG_HOME`/`$HOME` are read on every
introspection query.

VERIFIED — probe binary (scratchpad/argvrev, fw+completion linked, one
env-settable user field `--greeting`, one `Provides(Starter)` service
`test/svc` whose `--svc-marker` appears only when it is in the closure):

- Completion query, no XDG: `completionbash --cword 1 --line "probeapp
  --svc" -- probeapp --svc` → **no candidate**.
- Same query with `XDG_CONFIG_HOME` pointing at a dir holding
  `config.json = {"core":{"enable":["test/svc"]}}` → **`--svc-marker`**.
  The ambient environment pulled a service into the closure the
  env-blind view reports.
- Control: `PROBEAPP__ENABLE=test/svc` on the same completion query →
  no candidate (lookupEnv blinding works; the leak is the location, not
  the var).

Tension worth recording: a faithful completion SHOULD mirror the real
run's closure, and the real run legitimately reads XDG-located files, so
reading the file is desired — but the file's LOCATION being taken from
the interactive shell's environment is exactly what the absolute wording
forbids. Fix is a policy call: either blind `locations` in the envless
view too (completion then reports the companion/`/etc` closure only), or
soften both docs from "never read at all" to "no ambient env VAR overrides
a field; file discovery still honors XDG." Either way the current text
overstates the guarantee.

### D2 — stale security comment on `engine.Core` describes the closed hole as open (Low, doc-in-code)

`sxcli-conf/engine/types.go:75-85` (the Core doc comment) still reads:

- "All three are run-scoped (dump:\"-\")" — there are **four** fields
  (Config, WriteConfig, Help, ValidateConfig).
- "writeConfig and help **additionally** carry env:\"-\" … config keeps
  its env door (a legitimate deployment pattern; the pointed-at file
  still passes every gate)."

The struct itself is correct — `Config` carries `env:"-"` (L87), the very
change argv-only mandates. The comment predates the change and now
documents the env-config-redirect hole (attacker's `MYBIN__CONFIG=…`)
as still open. A maintainer trusting the comment could "restore" the
door and silently reopen the root-redirect vector. Code right, comment
security-wrong.

VERIFIED — read of types.go:75-91; `env:"-"` present on all four fields;
probe test F (`PROBEAPP__CONFIG=/…/evil.json ./argvrev` → ran with empty
greeting, env config path ignored) confirms the code, not the comment,
is authoritative.

## Doc drift

- `engine/types.go:75-85` Core comment — see D2.
- `engine/file.go:274` file-refusal message: `"config %s.%s: run-scoped,
  settable only by argument or environment"`. For a core field (all now
  `env:"-"`) it is argument-only; the message advertises an environment
  door that no longer exists for these fields. Generic across all
  `dump:"-"` fields, so low-priority, but misleading for core.
- argv-only doc table (L34) lists `--applets` as `env:"-"` / run-scoped.
  `--applets` is NOT implemented in fw (no such core field; the
  applets-listing design doc carries no STATUS block). The row is
  forward-looking; the table reads as if the field exists today.

## Non-defects investigated and cleared

- **Contribution fence (`rules/tags.ContribEnvRule`)** — VERIFIED via
  scratchpad/argvrev/fence: a core contribution field without `env:"-"`,
  one with an explicit env name, and a nested leaf without `env:"-"` are
  each rejected (3/3) at `engine.NewSchema`. Enforcement is structural,
  not tag-cosmetic: `tags/check.go:163-167` fails any Contribution-mode
  field lacking `env:"-"` (pos excepted, and pos is itself refused in
  Contribution mode).
- **All core Contribution structs comply** — `engine.Core` ×4
  (types.go:87-90), `coreControls` ×3 (fw/main.go:242-244),
  `upgradeKnobs` ×2 (fw/main.go:260-261) all carry `env:"-"`. A DERIVED
  env name for a core field is impossible: `schema.go:125` gates
  `DeriveEnv` on `!f.NoEnv`, and `env:"-"` sets `NoEnv` (check.go:170).
  No explicit env name survives the fence either (fence probe case 2).
- **env cannot enable/disable/override/redirect/serve** — probe battery:
  6 env spellings of ENABLE (`PROBEAPP__ENABLE`, `PROBEAPP_ENABLE`,
  `…__CORE__ENABLE`, `…_CORE_ENABLE`, `ENABLE`, `CORE__ENABLE`) all →
  service NOT pulled in; `PROBEAPP__HELP=1` → ran (no help served);
  `PROBEAPP__CONFIG=evil` → ignored. Positive controls: argv `--enable
  test/svc` pulls it in; argv `--config evil` honored; env user field
  `PROBEAPP__GREETING` honored.
- **enable/disable in files (doc: "allowed for now")** — VERIFIED: a
  config file with `core.enable` pulls the service into the closure
  (probe test H). Serving surfaces from a file are refused:
  `core.help`, `core.config`, `core.write-config` in a file each error
  out (`run-scoped, settable only by argument…` / `unknown key`).
- **alias/id squat** — VERIFIED in the built binary: a user service
  aliasing `system` → `alias "system" is reserved`
  (fw/builder.go:209, fw/catalog.go:192); registering id
  `sxcli.dev/fw/system` → `duplicate id` (system service is registered
  by fw.init first, imported-before-importer). `core` alias reserved by
  the same clause.
- **core-status forgery** — compile-impossible: `Registration.core()`
  is unexported (fw/catalog.go:100), `Registration.isCore` unexported,
  `registry.Descriptor.Core` lives in `internal/registry`. No exported
  seam sets it; a user package cannot mint a core member.
- **single env-blind door** — the fw `Introspector` is constructed in
  exactly one place, `systemservice.go:43`, reached only through the
  facade's `Introspector()`; `Introspector.rt` is unexported so no
  external code can build a functional one with a live `lookupEnv`. The
  facade `system.System` is an interface with a private impl
  (`systemService`), no public wiring seam. Runtime attaches `rt` once,
  in `run()` (main.go:58-62), by concrete-type assertion. (The env leak
  in D1 rides through this one door via `locations`, not a second door.)
- **completion reads no environment by any route** — grep across
  sxcli-completion for `Getenv`/`LookupEnv`/`lookupEnv`/`os.Environ`:
  none. Only `os.Args[0]`/`os.Stdout` used (bash.go:66/68, zsh.go:53/55).
  Both services inject `system.System` (bash/types.go:41,
  zsh/types.go:43) and go through the env-blind facade. Completion is
  clean; the D1 leak originates in fw's envless construction.
- fw + completion `go test ./...` green.

## Proposed tests

1. **fw — env-blind view is location-blind too (D1).** Drive
   `Introspector.Arguments` through the facade with a runtime whose
   `locations` closure is fed by a fake env var; assert the reported
   closure is independent of that var. Current
   `TestArgumentsNeverReadsEnvironment` pins only `lookupEnv` un-called;
   it does not cover `locations`' env derivation, which is the actual
   residual vector. If XDG-reading is ruled intended, invert the test to
   PIN that intent and fix the docs.
2. **conf — Core tag/comment drift guard (D2).** Reflect test asserting
   every `engine.Core` field carries both `env:"-"` and `dump:"-"`;
   fails the moment a field or the invariant drifts, so the comment can
   never again outlive the tags.
3. **fw — file cannot trigger a serving surface, one assertion per
   field.** `core.help`/`core.config`/`core.write-config`/
   `core.validate-config` in a config file each rejected. Locks the
   dump:"-" refusal against a future field losing the tag.
