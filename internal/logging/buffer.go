package logging

import (
	"context"
	"errors"
	"log/slog"
)

// NewBuffer builds an empty bootstrap buffer.
func NewBuffer() *Buffer {
	return &Buffer{store: &store{}}
}

// Enabled is unconditionally true: during startup nothing knows yet
// which levels the sinks will accept, so everything is captured.
func (b *Buffer) Enabled(context.Context, slog.Level) bool {
	return true
}

// Handle captures the record together with this view's op chain.
func (b *Buffer) Handle(ctx context.Context, record slog.Record) error {
	b.store.mu.Lock()
	b.store.entries = append(b.store.entries, entry{record: record.Clone(), ops: b.ops})
	b.store.mu.Unlock()
	return nil
}

// WithAttrs returns a view sharing the store, carrying one more chain
// step.
func (b *Buffer) WithAttrs(attrs []slog.Attr) slog.Handler {
	var out slog.Handler = b
	if len(attrs) > 0 {
		out = &Buffer{store: b.store, ops: b.extended(op{attrs: attrs})}
	}
	return out
}

// WithGroup returns a view sharing the store, carrying one more chain
// step.
func (b *Buffer) WithGroup(name string) slog.Handler {
	var out slog.Handler = b
	if name != "" {
		out = &Buffer{store: b.store, ops: b.extended(op{group: name})}
	}
	return out
}

// extended copies the op chain with one more step; views must never
// alias each other's backing arrays.
func (b *Buffer) extended(next op) []op {
	ops := make([]op, len(b.ops)+1)
	copy(ops, b.ops)
	ops[len(b.ops)] = next
	return ops
}

// Len returns the number of captured records across all views.
func (b *Buffer) Len() int {
	b.store.mu.Lock()
	n := len(b.store.entries)
	b.store.mu.Unlock()
	return n
}

// Replay delivers every captured record to target in arrival order,
// rebuilding each record's WithAttrs/WithGroup chain so it lands exactly
// as it would have live. Target errors are joined; records the target's
// Enabled rejects are skipped.
//
// Replay is meant to run once, at the bootstrap swap: the buffer does
// not drain — a second Replay re-delivers everything and captures keep
// accumulating — so discard the Buffer after the swap.
func (b *Buffer) Replay(target slog.Handler) error {
	b.store.mu.Lock()
	entries := make([]entry, len(b.store.entries))
	copy(entries, b.store.entries)
	b.store.mu.Unlock()
	ctx := context.Background()
	var errs []error
	for _, e := range entries {
		h := target
		for _, step := range e.ops {
			if step.group != "" {
				h = h.WithGroup(step.group)
			} else {
				h = h.WithAttrs(step.attrs)
			}
		}
		if h.Enabled(ctx, e.record.Level) {
			if err := h.Handle(ctx, e.record.Clone()); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
