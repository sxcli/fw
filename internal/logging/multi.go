package logging

import (
	"context"
	"errors"
	"log/slog"
)

// NewMulti builds the fan-out handler over the given children.
func NewMulti(children ...slog.Handler) *Multi {
	return &Multi{children: children}
}

// Enabled reports whether any child accepts the level, so the logger
// front end can skip building records nobody wants.
func (m *Multi) Enabled(ctx context.Context, level slog.Level) bool {
	var ok bool
	for _, child := range m.children {
		ok = ok || child.Enabled(ctx, level)
	}
	return ok
}

// Handle delivers the record to every child whose Enabled accepts its
// level. Child errors are joined and never prevent delivery to the
// remaining children.
func (m *Multi) Handle(ctx context.Context, record slog.Record) error {
	var errs []error
	for _, child := range m.children {
		if child.Enabled(ctx, record.Level) {
			if err := child.Handle(ctx, record.Clone()); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// WithAttrs returns a Multi over every child's derived view. Children
// are never copied — their WithAttrs shares the underlying sink, per the
// slog handler contract.
func (m *Multi) WithAttrs(attrs []slog.Attr) slog.Handler {
	var out slog.Handler = m
	if len(attrs) > 0 {
		children := make([]slog.Handler, len(m.children))
		for i, child := range m.children {
			children[i] = child.WithAttrs(attrs)
		}
		out = &Multi{children: children}
	}
	return out
}

// WithGroup returns a Multi over every child's derived view.
func (m *Multi) WithGroup(name string) slog.Handler {
	var out slog.Handler = m
	if name != "" {
		children := make([]slog.Handler, len(m.children))
		for i, child := range m.children {
			children[i] = child.WithGroup(name)
		}
		out = &Multi{children: children}
	}
	return out
}
