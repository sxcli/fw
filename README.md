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

```go
package main

import (
	"log/slog"
	"time"

	fw "sxcli.dev/fw"
	_ "sxcli.dev/fw/configfmt/yaml"  // optional: yaml config files
	_ "sxcli.dev/fw/sink/console"    // logging to stderr, on by default
)

type Config struct {
	Listen  string        `json:"listen" arg:"listen,l" usage:"address to serve on"`
	Timeout time.Duration `json:"timeout" arg:"timeout" usage:"request timeout"`
	Debug   bool          `json:"debug" arg:"debug" env:"-" usage:"verbose diagnostics"`
}

type Serve struct{ cfg Config }

func (s *Serve) Configured() error { return nil }
func (s *Serve) Run() int {
	slog.Info("serving", "listen", s.cfg.Listen, "timeout", s.cfg.Timeout)
	return 0
}

func main() {
	s := &Serve{cfg: Config{Listen: ":8080", Timeout: 30 * time.Second}}
	fw.Register("serve", s, fw.WithConfig(&s.cfg))
	fw.Main()
}
```

One registered applet means no subcommands, no ceremony — the binary
*is* the applet, and all of these set the same field:

```console
$ mytool --listen :9090 --timeout 5s
$ SERVE_LISTEN=:9090 mytool
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
| `arg:"long[,short]"` | opt-in CLI argument: `--long value`, `--long=value`, `-s value` |
| `env:"NAME"` | explicit env var; omitted → derived (`APPLETID_` + long name); `env:"-"` → argument-only |
| `usage:"..."` | help text, translation-ready |
| `dump:"-"` | run-scoped: excluded from generated config files and refused from them |

Config files are discovered next to the real binary
(`<dir>/<applet>-config.<ext>`), in `/etc/<applet>/` (or
`%ProgramData%`), and in the XDG user config directory — merged in that
order — or replaced wholesale by an explicit `--config path`. JSON is
native; importing `sxcli.dev/fw/configfmt/yaml` adds `.yaml`/`.yml`,
and the format-provider interface is public for anything else.

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
`init()`; fields tagged `inject` receive other services by interface or
concrete type:

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

## Multi-applet binaries

The same registration scales to busybox-style multi-call binaries:
register several applets and the framework dispatches by first argument
(`mybox serve`) or by binary name (`ln -s mybox serve; ./serve`). Each
applet pays only for its own dependency closure — a five-applet binary
running `serve` never touches the other four applets' services or
arguments.

## Logging

Log sinks are ordinary services implementing `slog.Handler`. The
console sink (stderr, text or json) is on by default when imported;
file and syslog/journald sinks activate per invocation:

```console
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
