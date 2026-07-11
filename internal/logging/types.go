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

// Package logging implements the framework's log fan-out (Multi) and the
// startup bootstrap handler (Buffer). Both are plain slog.Handlers and
// know nothing about services: sinks arrive as the handler values the
// resolved closure provides.
//
// Multi is deliberately synchronous: a record is delivered on the
// caller's goroutine to every accepting child. Sink authors must keep
// Handle prompt and apply their own I/O deadlines — a decoupling async
// decorator is a possible future addition, not core machinery.
package logging

import (
	"log/slog"
	"sync"
)

// Multi fans one record out to every child whose Enabled accepts its
// level. Child errors are joined; one failing sink never blocks the
// others.
type Multi struct {
	children []slog.Handler
}

// op is one WithAttrs/WithGroup step a Buffer view carries.
type op struct {
	group string      // non-empty: a WithGroup step
	attrs []slog.Attr // otherwise: a WithAttrs step
}

// entry is one captured record together with the view chain it arrived
// through.
type entry struct {
	record slog.Record
	ops    []op
}

// store is the record store shared by every view of one Buffer.
type store struct {
	mu      sync.Mutex
	entries []entry
}

// Buffer is the bootstrap slog.Handler installed as the default before
// the sinks exist. It captures every record — all levels — together
// with the WithAttrs/WithGroup chain it arrived through, so Replay can
// deliver it to the real handler exactly as it would have landed live.
type Buffer struct {
	store *store
	ops   []op
}
