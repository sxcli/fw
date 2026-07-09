// Package fail provides the shared startup-error collector. Every
// internal phase — registration, resolution, injection, configuration —
// records its violations into one Collector owned by the framework core,
// so startup can fail once and report every problem together.
package fail

// Collector accumulates startup violations in occurrence order. The
// zero value is ready to use. Callers gate phases by comparing Len
// snapshots taken before and after a phase.
type Collector struct {
	errs []error
}
