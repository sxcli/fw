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

// Package console provides the console log sink: a slog.Handler service
// writing text or json records to stderr (default) or stdout. It is an
// opt-in service — enabled with --enable console or pulled by a
// dependency; a binary that imports it but enables nothing falls to
// the framework's raw stderr logging floor:
//
//	fw.Builder().Accept(console.ID, …)   // or any AcceptAll composition
package console

import (
	"log/slog"

	"sxcli.dev/fw"
)

// Config is the console sink configuration, section "console".
type Config struct {
	Version uint32 `json:"version"`
	Level   string `json:"level" conf:"console-level" usage:"minimum level logged to the console (debug, info, warn, error)"`
	Format  string `json:"format" conf:"console-format" usage:"console log format: text or json"`
	Output  string `json:"output" conf:"console-output" usage:"console log destination: stderr or stdout"`
}

// Console is the sink service. Until Configured builds the inner
// handler the sink is inert: disabled, handling nothing.
type Console struct {
	cfg   Config
	level slog.LevelVar // shared with the inner handler so a future config reload can retune it live
	inner slog.Handler
}

var _ fw.Starter = (*Console)(nil)
var _ fw.Configurable = (*Console)(nil)
var _ slog.Handler = (*Console)(nil)
