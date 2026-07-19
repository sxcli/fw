# sxcli — Simple Extensible CLI framework

`sxcli.dev/fw` is a Go framework for building command-line tools and
services around one idea: **your configuration struct is your entire
interface**. Declare a struct once and every field is simultaneously a
command-line argument, an environment variable, and a config-file key —
merged with clear precedence, validated strictly, and handed to your
code filled in.

On top of that sit a service model with dependency injection, an
ordered lifecycle, `log/slog` logging with pluggable sinks, translation
hooks, Windows service support — and, when you want it, busybox-style
multi-applet binaries.

Guides, demos and the [API reference](https://sxcli.dev/s/api) live at
[sxcli.dev](https://sxcli.dev); the package documentation is also on
[pkg.go.dev](https://pkg.go.dev/sxcli.dev/fw).

```go
package main

import (
	"log/slog"
	"time"

	"sxcli.dev/fw"
)

type Config struct {
	Version uint32        `json:"version"` // the schema's version, for config migrations
	Listen  string        `json:"listen" conf:"listen,l" usage:"address to serve on"`
	Timeout time.Duration `json:"timeout" conf:"timeout" usage:"request timeout"`
	Debug   bool          `json:"debug" conf:"debug" env:"-" usage:"verbose diagnostics"`
}

type Serve struct{ cfg Config }

func (s *Serve) Configured() error { return nil }
func (s *Serve) Run() int {
	slog.Info("serving", "listen", s.cfg.Listen, "timeout", s.cfg.Timeout)
	return 0
}

func main() {
	fw.Solo(fw.NewRegistration("example.com/mytool/serve",
		func() *Serve { return &Serve{cfg: Config{Version: 1, Listen: ":8080", Timeout: 30 * time.Second}} },
		func(s *Serve) *Config { return &s.cfg }).
		Alias("serve"))
}
```

A registration carries two names: the **id** (import-path-shaped,
unique by construction — what code says in inject tags and
compositions) and the **alias** (the short operator name — what humans
say). `Solo` is the single-applet front door: no subcommands, no
ceremony — the binary *is* the applet, and all of these set the same
field:

```console
$ mytool --listen :9090 --timeout 5s
$ SERVE__LISTEN=:9090 mytool
$ echo '{"serve": {"listen": ":9090"}}' > /etc/serve/config.json && mytool
```

## Configuration

**One struct, four sources**, least to most important:

```
struct defaults  <  config files  <  environment  <  arguments
```

Field tags declare the whole surface:

| Tag | Meaning |
| --- | --- |
| `json:"name"` | the config-file key (required on every exported field) |
| `conf:"long[,short]"` | the operator name: grants `--long value`, `--long=value`, `-s value` AND feeds the env name |
| `env:"NAME"` | explicit env var, verbatim; omitted → derived (`ALIAS__` + name, `__` at path boundaries); `env:"-"` → no env |
| `usage:"..."` | help text, translation-ready |
| `dump:"-"` | run-scoped: excluded from generated config files and refused from them |

Config files are discovered next to the real binary
(`<dir>/<alias>-config.<ext>`), in `/etc/<alias>/` (or
`%ProgramData%`), and in the XDG user config directory — merged in that
order — or replaced wholesale by an explicit `--config path`. JSON is
native; accepting `sxcli.dev/fw/configfmt/yaml` into the composition
adds `.yaml`/`.yml`, and the format-provider interface is public for
anything else.

Built-in conveniences:

- `--help` prints the full argument schema with current effective values.
- `--write-config` emits the merged configuration — to stdout as JSON,
  or to `--config target.yaml` in the format the extension names. An
  existing target is loaded first, making it a config normalizer.
- Durations accept `5s`, `5000ms`, `1h30m` — never bare numbers, in any
  source.
- Slices: repeat the flag, comma-separate the env value, use arrays in
  files.
- GNU-style parsing: `--key=value`, short-flag bundling (`-vvq 3`),
  `--` terminator, trailing positionals.

And deliberate strictness, because silent misconfiguration is the worst
bug class: unknown arguments and unknown config keys are startup
errors; config files are size-capped and must be regular files (a FIFO
or device never blocks startup); the binary-companion config refuses
symlink games entirely; and run-scoped flags like `--help` cannot be
smuggled in via files or environment.

## Services and dependency injection

Applets are just services. Any package can register services in
`init()` — registration fills a process-wide **catalog** of factories
and declarations, and the binary decides what participates. Fields
tagged `inject` receive other services by interface or concrete type:

```go
type Store interface{ Get(key string) string }

type Serve struct {
	cfg   Config
	Store Store         `inject:""`          // first registered Store
	Extra []slog.Handler `inject:";optional"` // all matching, if any
}
```

The framework computes the dependency closure of the dispatched applet,
injects fields, then drives the lifecycle in dependency order:
`Configured()` → `Start()` → the applet's `Run()` → `Stop()` in exact
reverse. Services never required stay cold — never configured, never
started, their arguments not even parsed.

Operators can recompose without recompiling: `--disable sqlite
--enable mysql --override sqlite=mysql` remove, force, and remap
services from the command line, environment, or config file.

## Composition and multi-applet binaries

Beyond `Solo` sits the Builder: the binary names what it takes from
the catalog, and imports are justified by the `ID` constants packages
export — no blank-import magic:

```go
import (
	"sxcli.dev/fw"
	"sxcli.dev/fw/sink/console"
	"example.com/box/grep"
	"example.com/box/serve"
)

func main() {
	fw.Builder().
		Accept(serve.ID, grep.ID, console.ID).
		Order(serve.ID, grep.ID). // listing order; ranked services win ties
		Main()
}
```

(`fw.Main()` is the take-everything shorthand — `AcceptAll` composed
and run.) Several accepted applets make a busybox-style multi-call
binary: the framework dispatches by first argument (`mybox serve`) or
by binary name (`ln -s mybox serve; ./serve`). Each applet pays only
for its own dependency closure — a five-applet binary running `serve`
never touches the other four applets' services or arguments.
`Builder.Alias` renames operator surfaces per composition (and pins
them against upstream changes); ambiguity is never resolved silently —
two unranked candidates for one dependency slot fail the build by
name.

## Logging

Logging always works: with no sink enabled, records land on stderr
through a built-in plain handler — the logging floor. (No silence
switch either; redirect stderr if you want quiet.) Richer sinks are
ordinary services implementing `slog.Handler` — accepted into the
composition like any other service, activated per invocation —
console (stderr/stdout, text or json), file, and syslog/journald:

```console
$ mytool --enable console --console-format json
$ mytool --enable logfile --logfile-path /var/log/mytool.log
$ mytool --enable syslog          # journald picks this up under systemd
```

Everything logged during startup is buffered and replayed into the real
sinks once they are configured — nothing is lost, even from `init()`.

## Windows services

An applet implementing `SCMApplet` runs under the Service Control
Manager with the same configuration pipeline, and still works as a
plain console program. `--scm-debug` (a deliberate build-time opt-in:
`fw.Enable(fw.FeatureSCMDebug)`) runs the service path in a terminal
for testing — the framework's own test suite drives it under Wine.

## Hardening

The binary author decides which core features exist at all:

```go
fw.Suppress(fw.FeatureConfigFile, fw.FeatureOverride) // no --config, no rewiring
fw.MaxConfigSize(64 << 10)                            // tighter than the 1 MiB default
```

Suppressed features vanish: the argument becomes unknown, the env var
is never read, and a config file mentioning them fails loudly.

## Status

v0: the API is settling and may still move. Module path `sxcli.dev/fw`,
Go 1.26+. Licensed under Apache-2.0.
