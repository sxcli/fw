// Package sxclifw is the sxcli framework — sxcli stands for Simple
// Extensible CLI — for building busybox-style single-binary tools. A
// consumer registers services (applets are just services that
// implement a specific interface) from package init() functions and calls
// Main(); the framework dispatches by argv[0] or by the first subcommand
// argument, resolves the dependency closure of the chosen applet, drives
// configuration from arguments, environment variables and config files,
// and runs the service lifecycle around the applet.
package sxclifw

// Stopper is the base lifecycle interface. Stop is called once, in exact
// reverse order of the successful Start calls, after the applet returns
// (or when startup is aborted after this service was started). Errors
// returned from Stop are logged; they never change the process exit code
// and never prevent the remaining Stop calls.
type Stopper interface {
	Stop() error
}

// Starter is implemented by services that need a running phase: anything
// startable must be stoppable. Start is called sequentially, in dependency
// order, after every closure member has been configured and just before
// the applet runs. A Start error aborts startup: already-started services
// are stopped in reverse order and the process exits non-zero.
type Starter interface {
	Stopper
	Start() error
}

// AlwaysOn services are active regardless of the applet's dependency
// closure. It embeds Starter, guaranteeing that an always-on service gets
// a full lifecycle and therefore a way to be informed on stop. The
// interface is structurally identical to Starter — always-on status comes
// solely from the explicit Provides[AlwaysOn]() declaration at
// registration.
//
// WARNING: an AlwaysOn service is configured, started, and stopped for
// EVERY applet in the binary, whether that applet needs it or not. It
// taxes every invocation with its startup cost, its configuration surface,
// and its failure modes. It SHOULD NOT be used lightly, if at all — almost
// every service belongs in the normal dependency closure instead. AlwaysOn
// exists for framework-level infrastructure (config format providers, log
// sinks) and little else. The framework reserves the right to disable or
// remove AlwaysOn support in a future version; do not build designs that
// depend on it.
type AlwaysOn interface {
	Starter
}

// Configurable is implemented by services that own a Configuration struct
// (registered via WithConfig). The framework fills that registered struct
// in place with the merged configuration (defaults, config files,
// environment, arguments — least to most important) and then calls
// Configured as a notification; there is never a second config instance.
//
// Configured is called sequentially, in dependency order, after dependency
// fields have been injected — but before anything is started, so injected
// dependencies must not be treated as running yet. A Configured error
// aborts startup.
type Configurable interface {
	Configured() error
}

// ConfigurationUpdater is reserved: it will notify a service that its
// configuration struct has been re-filled with updated values. What
// triggers an update (file watch, signal, API) is deliberately undecided;
// no update is ever delivered in the current version.
type ConfigurationUpdater interface {
	ConfigurationUpdated() error
}

// Applet is a dispatchable entry point. The framework brackets Run with
// the application lifecycle: every closure member is configured and
// started before Run is invoked, and stopped in reverse order right after
// it returns. The process exits with Run's return value.
//
// An applet must not implement Starter or Stopper — registering one that
// does is a registration error. Panics inside Run are not recovered: the
// applet owns its own recovery and must return its error code from Run.
type Applet interface {
	Configurable
	Run() int
}
