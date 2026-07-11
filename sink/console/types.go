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
// writing text or json records to stderr (default) or stdout. It
// registers itself as always-on, so a binary that imports it has sane
// log output with no further wiring:
//
//	import _ "sxcli.dev/fw/sink/console"
package console

import (
	"log/slog"

	sxclifw "sxcli.dev/fw"
)

// Config is the console sink configuration, section "console".
type Config struct {
	Level  string `json:"level" arg:"console-level" usage:"minimum level logged to the console (debug, info, warn, error)"`
	Format string `json:"format" arg:"console-format" usage:"console log format: text or json"`
	Output string `json:"output" arg:"console-output" usage:"console log destination: stderr or stdout"`
}

// Console is the sink service. Until Configured builds the inner
// handler the sink is inert: disabled, handling nothing.
type Console struct {
	cfg   Config
	level slog.LevelVar // shared with the inner handler so a future config reload can retune it live
	inner slog.Handler
}

var _ sxclifw.AlwaysOn = (*Console)(nil)
var _ sxclifw.Configurable = (*Console)(nil)
var _ slog.Handler = (*Console)(nil)
