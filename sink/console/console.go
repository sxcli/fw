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

package console

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"sxcli.dev/fw"
)

// ID is the console sink's identity; operators call it "console".
const ID = "sxcli.dev/fw/sink/console"

func init() {
	fw.NewRegistration(ID, func() *Console {
		return &Console{cfg: Config{Level: "info", Format: "text", Output: "stderr"}}
	}, func(c *Console) *Config { return &c.cfg }).
		Alias("console").
		Provides(fw.Iface[slog.Handler]()).
		Metadata(&fw.Metadata{
			Description: "console log sink: writes slog records to stderr (default) or stdout; opt-in — enable it with --enable console or a dependency, otherwise the framework's raw stderr fallback applies",
			Fields: map[string]any{
				"Level": fw.FieldMetadata[string]{
					// deliberately no Allowed: slog accepts offsets like
					// "warn+2" and any case, so the domain is open
					Doc: "any form slog.Level understands: debug, info, warn, error, case-insensitive, offsets like warn+2",
				},
				"Format": fw.FieldMetadata[string]{Allowed: []string{"text", "json"}},
				"Output": fw.FieldMetadata[string]{Allowed: []string{"stderr", "stdout"}},
			},
		}).
		Register()
}

// Configured builds the inner handler from the merged configuration.
func (s *Console) Configured() error {
	var err error
	if s.cfg.Output == "stderr" {
		err = s.build(os.Stderr)
	} else if s.cfg.Output == "stdout" {
		err = s.build(os.Stdout)
	} else {
		err = fmt.Errorf("console: invalid output %q (stderr or stdout)", s.cfg.Output)
	}
	return err
}

// build constructs the inner handler over w; split from Configured so
// tests can inject a writer.
func (s *Console) build(w io.Writer) error {
	var err error
	var level slog.Level
	if err = level.UnmarshalText([]byte(s.cfg.Level)); err == nil {
		s.level.Set(level)
		opts := &slog.HandlerOptions{Level: &s.level}
		if s.cfg.Format == "text" {
			s.inner = slog.NewTextHandler(w, opts)
		} else if s.cfg.Format == "json" {
			s.inner = slog.NewJSONHandler(w, opts)
		} else {
			err = fmt.Errorf("console: invalid format %q (text or json)", s.cfg.Format)
		}
	} else {
		err = fmt.Errorf("console: invalid level %q: %v", s.cfg.Level, err)
	}
	return err
}

// Start is a no-op: the console needs no acquisition.
func (s *Console) Start() error {
	return nil
}

// Stop is a no-op: stderr and stdout are not ours to close.
func (s *Console) Stop() error {
	return nil
}

// Enabled reports whether the record would be logged; an unconfigured
// sink accepts nothing.
func (s *Console) Enabled(ctx context.Context, level slog.Level) bool {
	return s.inner != nil && s.inner.Enabled(ctx, level)
}

// Handle delegates to the inner handler.
func (s *Console) Handle(ctx context.Context, record slog.Record) error {
	var err error
	if s.inner != nil {
		err = s.inner.Handle(ctx, record)
	}
	return err
}

// WithAttrs returns a derived view sharing the underlying output, per
// the sink-author contract.
func (s *Console) WithAttrs(attrs []slog.Attr) slog.Handler {
	var out slog.Handler = s
	if s.inner != nil && len(attrs) > 0 {
		out = s.inner.WithAttrs(attrs)
	}
	return out
}

// WithGroup returns a derived view sharing the underlying output.
func (s *Console) WithGroup(name string) slog.Handler {
	var out slog.Handler = s
	if s.inner != nil && name != "" {
		out = s.inner.WithGroup(name)
	}
	return out
}
