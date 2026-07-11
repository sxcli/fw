// Copyright 2026 Plamen K. Kosseff
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
// front end can skip building records nobody wants. This is the hottest
// path in logging; the loop stops at the first accepting child.
func (m *Multi) Enabled(ctx context.Context, level slog.Level) bool {
	ok := false
	for i := 0; i < len(m.children) && !ok; i++ {
		ok = m.children[i].Enabled(ctx, level)
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
