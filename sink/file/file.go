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

package file

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	sxclifw "sxcli.dev/fw"
)

func init() {
	f := &File{cfg: Config{Level: "info", Format: "text", Mode: "0600"}}
	sxclifw.Register("logfile", f,
		sxclifw.Provides[slog.Handler](),
		sxclifw.WithConfig(&f.cfg),
	)
}

// Configured validates the configuration and — as its last act — opens
// the file and builds the inner handler, so the sink is operational for
// the buffer replay at the multihandler swap.
func (s *File) Configured() error {
	var err error
	var level slog.Level
	var mode uint64
	if s.cfg.Path == "" {
		err = fmt.Errorf("logfile: a path is required when the sink is enabled")
	} else if err = level.UnmarshalText([]byte(s.cfg.Level)); err != nil {
		err = fmt.Errorf("logfile: invalid level %q: %v", s.cfg.Level, err)
	} else if s.cfg.Format != "text" && s.cfg.Format != "json" {
		err = fmt.Errorf("logfile: invalid format %q (text or json)", s.cfg.Format)
	} else if mode, err = strconv.ParseUint(s.cfg.Mode, 8, 12); err != nil {
		err = fmt.Errorf("logfile: invalid mode %q: must be octal permissions", s.cfg.Mode)
	} else {
		var out *os.File
		if out, err = os.OpenFile(s.cfg.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, os.FileMode(mode)); err == nil {
			s.level.Set(level)
			s.out = out
			opts := &slog.HandlerOptions{Level: &s.level}
			if s.cfg.Format == "json" {
				s.inner = slog.NewJSONHandler(out, opts)
			} else {
				s.inner = slog.NewTextHandler(out, opts)
			}
		} else {
			err = fmt.Errorf("logfile: %v", err)
		}
	}
	return err
}

// Start is a no-op: the file was opened in Configured so the sink could
// capture the startup replay. The sink stays a Starter because only
// started Starters receive Stop.
func (s *File) Start() error {
	return nil
}

// Stop closes the file.
func (s *File) Stop() error {
	var err error
	if s.out != nil {
		err = s.out.Close()
		s.out = nil
		s.inner = nil
	}
	return err
}

// Enabled reports whether the record would be logged; an unconfigured
// sink accepts nothing.
func (s *File) Enabled(ctx context.Context, level slog.Level) bool {
	return s.inner != nil && s.inner.Enabled(ctx, level)
}

// Handle delegates to the inner handler.
func (s *File) Handle(ctx context.Context, record slog.Record) error {
	var err error
	if s.inner != nil {
		err = s.inner.Handle(ctx, record)
	}
	return err
}

// WithAttrs returns a derived view sharing the underlying file, per the
// sink-author contract.
func (s *File) WithAttrs(attrs []slog.Attr) slog.Handler {
	var out slog.Handler = s
	if s.inner != nil && len(attrs) > 0 {
		out = s.inner.WithAttrs(attrs)
	}
	return out
}

// WithGroup returns a derived view sharing the underlying file.
func (s *File) WithGroup(name string) slog.Handler {
	var out slog.Handler = s
	if s.inner != nil && name != "" {
		out = s.inner.WithGroup(name)
	}
	return out
}
