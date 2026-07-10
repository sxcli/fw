package console

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	sxclifw "sxcli.dev/fw"
)

func init() {
	c := &Console{cfg: Config{Level: "info", Format: "text", Output: "stderr"}}
	sxclifw.Register("console", c,
		sxclifw.Provides[slog.Handler](),
		sxclifw.Provides[sxclifw.AlwaysOn](),
		sxclifw.WithConfig(&c.cfg),
	)
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
