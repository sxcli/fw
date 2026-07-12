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

//go:build unix

package syslog

import (
	"context"
	"fmt"
	"log/slog"
	"log/syslog"
	"strings"

	sxclifw "sxcli.dev/fw"
)

func init() {
	s := &Syslog{cfg: Config{Level: "info", Format: "text", Facility: "daemon"}}
	sxclifw.Register("syslog", s,
		sxclifw.Provides[slog.Handler](),
		sxclifw.WithConfig(&s.cfg),
		sxclifw.WithMetadata(&sxclifw.Metadata{
			Description: "syslog sink: writes slog records to the local syslog socket (journald under systemd) or a remote server; cold until enabled",
			Fields: map[string]any{
				"Level": sxclifw.FieldMetadata[string]{
					Doc: "any form slog.Level understands: debug, info, warn, error, case-insensitive, offsets like warn+2",
				},
				"Format": sxclifw.FieldMetadata[string]{Allowed: []string{"text", "json"}},
				"Facility": sxclifw.FieldMetadata[string]{Allowed: []string{
					"kern", "user", "mail", "daemon", "auth", "syslog",
					"lpr", "news", "uucp", "cron", "authpriv", "ftp",
					"local0", "local1", "local2", "local3",
					"local4", "local5", "local6", "local7",
				}},
				"Network": sxclifw.FieldMetadata[string]{
					// open domain: any network syslog.Dial accepts
					// (udp, tcp, unixgram, ...); empty means the local
					// socket
					Doc: "remote syslog network; empty for the local socket, otherwise anything syslog.Dial accepts (udp, tcp, ...)",
				},
				"Tag": sxclifw.FieldMetadata[string]{
					Doc: "syslog tag; empty defaults to the process name",
				},
				"Address": sxclifw.FieldMetadata[string]{
					Doc: "host:port of the remote server; set together with network",
				},
			},
		}),
	)
}

var facilities = map[string]syslog.Priority{
	"kern": syslog.LOG_KERN, "user": syslog.LOG_USER, "mail": syslog.LOG_MAIL,
	"daemon": syslog.LOG_DAEMON, "auth": syslog.LOG_AUTH, "syslog": syslog.LOG_SYSLOG,
	"lpr": syslog.LOG_LPR, "news": syslog.LOG_NEWS, "uucp": syslog.LOG_UUCP,
	"cron": syslog.LOG_CRON, "authpriv": syslog.LOG_AUTHPRIV, "ftp": syslog.LOG_FTP,
	"local0": syslog.LOG_LOCAL0, "local1": syslog.LOG_LOCAL1,
	"local2": syslog.LOG_LOCAL2, "local3": syslog.LOG_LOCAL3,
	"local4": syslog.LOG_LOCAL4, "local5": syslog.LOG_LOCAL5,
	"local6": syslog.LOG_LOCAL6, "local7": syslog.LOG_LOCAL7,
}

// dropTimeLevel removes the top-level time and level attributes from
// rendered lines: syslog stamps a timestamp itself and severity travels
// in the message priority.
func dropTimeLevel(groups []string, a slog.Attr) slog.Attr {
	out := a
	if len(groups) == 0 && (a.Key == slog.TimeKey || a.Key == slog.LevelKey) {
		out = slog.Attr{}
	}
	return out
}

// Configured validates the configuration and — as its last act — dials
// the syslog socket and builds the inner formatter, so the sink is
// operational for the buffer replay at the multihandler swap.
func (s *Syslog) Configured() error {
	var err error
	var level slog.Level
	facility, knownFacility := facilities[s.cfg.Facility]
	if err = level.UnmarshalText([]byte(s.cfg.Level)); err != nil {
		err = fmt.Errorf("syslog: invalid level %q: %v", s.cfg.Level, err)
	} else if s.cfg.Format != "text" && s.cfg.Format != "json" {
		err = fmt.Errorf("syslog: invalid format %q (text or json)", s.cfg.Format)
	} else if !knownFacility {
		err = fmt.Errorf("syslog: unknown facility %q", s.cfg.Facility)
	} else if (s.cfg.Network == "") != (s.cfg.Address == "") {
		err = fmt.Errorf("syslog: network and address must be set together")
	} else {
		var out *syslog.Writer
		if out, err = syslog.Dial(s.cfg.Network, s.cfg.Address, facility|syslog.LOG_INFO, s.cfg.Tag); err == nil {
			s.level.Set(level)
			s.out = out
			opts := &slog.HandlerOptions{Level: &s.level, ReplaceAttr: dropTimeLevel}
			if s.cfg.Format == "json" {
				s.inner = slog.NewJSONHandler(&s.buf, opts)
			} else {
				s.inner = slog.NewTextHandler(&s.buf, opts)
			}
		} else {
			err = fmt.Errorf("syslog: %v", err)
		}
	}
	return err
}

// Start is a no-op: the socket was dialed in Configured so the sink
// could capture the startup replay. The sink stays a Starter because
// only started Starters receive Stop.
func (s *Syslog) Start() error {
	return nil
}

// Stop closes the syslog connection.
func (s *Syslog) Stop() error {
	var err error
	if s.out != nil {
		err = s.out.Close()
		s.out = nil
		s.inner = nil
	}
	return err
}

// deliver renders one record through the given formatter and emits it
// with the severity mapped from the record level. The buffer and the
// connection are shared by every view, hence the lock.
func (s *Syslog) deliver(ctx context.Context, record slog.Record, formatter slog.Handler) error {
	s.mu.Lock()
	s.buf.Reset()
	err := formatter.Handle(ctx, record)
	if err == nil {
		line := strings.TrimRight(s.buf.String(), "\n")
		if record.Level >= slog.LevelError {
			err = s.out.Err(line)
		} else if record.Level >= slog.LevelWarn {
			err = s.out.Warning(line)
		} else if record.Level >= slog.LevelInfo {
			err = s.out.Info(line)
		} else {
			err = s.out.Debug(line)
		}
	}
	s.mu.Unlock()
	return err
}

// Enabled reports whether the record would be logged; an unconfigured
// sink accepts nothing.
func (s *Syslog) Enabled(ctx context.Context, level slog.Level) bool {
	return s.inner != nil && s.inner.Enabled(ctx, level)
}

// Handle delegates to the shared delivery path.
func (s *Syslog) Handle(ctx context.Context, record slog.Record) error {
	var err error
	if s.inner != nil {
		err = s.deliver(ctx, record, s.inner)
	}
	return err
}

// WithAttrs returns a derived view sharing the connection, per the
// sink-author contract.
func (s *Syslog) WithAttrs(attrs []slog.Attr) slog.Handler {
	var out slog.Handler = s
	if s.inner != nil && len(attrs) > 0 {
		out = view{parent: s, inner: s.inner.WithAttrs(attrs)}
	}
	return out
}

// WithGroup returns a derived view sharing the connection.
func (s *Syslog) WithGroup(name string) slog.Handler {
	var out slog.Handler = s
	if s.inner != nil && name != "" {
		out = view{parent: s, inner: s.inner.WithGroup(name)}
	}
	return out
}

func (v view) Enabled(ctx context.Context, level slog.Level) bool {
	return v.inner.Enabled(ctx, level)
}

func (v view) Handle(ctx context.Context, record slog.Record) error {
	return v.parent.deliver(ctx, record, v.inner)
}

func (v view) WithAttrs(attrs []slog.Attr) slog.Handler {
	var out slog.Handler = v
	if len(attrs) > 0 {
		out = view{parent: v.parent, inner: v.inner.WithAttrs(attrs)}
	}
	return out
}

func (v view) WithGroup(name string) slog.Handler {
	var out slog.Handler = v
	if name != "" {
		out = view{parent: v.parent, inner: v.inner.WithGroup(name)}
	}
	return out
}
