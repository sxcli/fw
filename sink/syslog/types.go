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

// Package syslog provides the syslog log sink: a slog.Handler service
// writing records to the local syslog socket (default) or a remote
// syslog server. Under systemd the local socket is journald, so records
// land in journalctl with severity and tag intact — no dedicated
// journal protocol needed for ingestion. It is an ordinary service —
// cold until an applet requires it or the operator enables it:
//
//	import _ "sxcli.dev/fw/sink/syslog"
//	// then: mybox applet --enable syslog
//
// The sink is unavailable on non-unix platforms: importing the package
// there is harmless and registers nothing.
//
// Caveat: the stdlib syslog writer exposes no write deadlines, so a
// remote tcp target cannot fully honor the prompt-Handle sink contract;
// the local socket (the default) is unaffected.
package syslog

import (
	"bytes"
	"log/slog"
	"log/syslog"
	"sync"

	"sxcli.dev/fw"
)

// ID is the syslog sink's identity; operators call it "syslog". The
// sink is unix-only; the identity is portable.
const ID = "sxcli.dev/fw/sink/syslog"

// Config is the syslog sink configuration, section "syslog".
type Config struct {
	Level    string `json:"level" arg:"syslog-level" usage:"minimum level logged to syslog (debug, info, warn, error)"`
	Format   string `json:"format" arg:"syslog-format" usage:"syslog message format: text or json"`
	Tag      string `json:"tag" arg:"syslog-tag" usage:"syslog tag; defaults to the process name"`
	Facility string `json:"facility" arg:"syslog-facility" usage:"syslog facility (daemon, user, auth, local0..local7, ...)"`
	Network  string `json:"network" arg:"syslog-network" usage:"remote syslog network (udp, tcp); empty means the local socket"`
	Address  string `json:"address" arg:"syslog-address" usage:"remote syslog address as host:port; set together with network"`
}

// Syslog is the sink service. Severity travels per message, so Handle
// renders the record through the inner formatter into a guarded buffer
// and emits the line with the severity mapped from the record level.
// The formatter drops time= and level= — syslog stamps both itself.
type Syslog struct {
	cfg   Config
	level slog.LevelVar // shared with the inner formatter for future live retuning
	mu    sync.Mutex
	buf   bytes.Buffer
	out   *syslog.Writer
	inner slog.Handler // renders into buf
}

// view is a derived WithAttrs/WithGroup handler sharing the parent's
// connection and buffer, with its own derived formatter.
type view struct {
	parent *Syslog
	inner  slog.Handler
}

var _ fw.Starter = (*Syslog)(nil)
var _ fw.Configurable = (*Syslog)(nil)
var _ slog.Handler = (*Syslog)(nil)
var _ slog.Handler = view{}
