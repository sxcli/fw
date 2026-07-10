// Package file provides the log-file sink: a slog.Handler service
// writing text or json records to an append-only file. It is an
// ordinary service — cold until an applet requires it or the operator
// enables it (core `enable`) — never always-on:
//
//	import _ "sxcli.dev/fw/sink/file"
//	// then: mybox applet --enable logfile --logfile-path /var/log/box.log
//
// There is no rotation: the file is opened O_APPEND and external
// rotation (logrotate copytruncate) works as-is. Reopen-on-reload may
// come with ConfigurationUpdated.
package file

import (
	"log/slog"
	"os"

	sxclifw "sxcli.dev/fw"
)

// Config is the file sink configuration, section "logfile". Path has no
// default: an enabled file sink without an explicit path is a startup
// error — guessing one would be worse.
type Config struct {
	Path   string `json:"path" arg:"logfile-path" usage:"log file path (required when the sink is enabled)"`
	Level  string `json:"level" arg:"logfile-level" usage:"minimum level logged to the file (debug, info, warn, error)"`
	Format string `json:"format" arg:"logfile-format" usage:"file log format: text or json"`
	Mode   string `json:"mode" arg:"logfile-mode" usage:"octal permissions for a newly created log file"`
}

// File is the sink service. Per the sink contract it is fully
// operational when Configured returns: validation happens first and the
// open is Configured's last act, so a bad config never creates a file.
// Stop closes it.
type File struct {
	cfg   Config
	level slog.LevelVar // shared with the inner handler for future live retuning
	out   *os.File
	inner slog.Handler
}

var _ sxclifw.Starter = (*File)(nil)
var _ sxclifw.Configurable = (*File)(nil)
var _ slog.Handler = (*File)(nil)
