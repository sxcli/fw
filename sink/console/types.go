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
