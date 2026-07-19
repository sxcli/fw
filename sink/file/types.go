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

// Package file provides the log-file sink: a slog.Handler service
// writing text or json records to an append-only file. It is an
// ordinary service — cold until an applet requires it or the operator
// enables it (core `enable`), exactly like the console sink now:
//
//	fw.Builder().Accept(file.ID, …)   // or any AcceptAll composition
//	// then: mybox applet --enable logfile --logfile-path /var/log/box.log
//
// There is no rotation: the file is opened O_APPEND and external
// rotation (logrotate copytruncate) works as-is. Reopen-on-reload may
// come with ConfigurationUpdated.
package file

import (
	"log/slog"
	"os"

	"sxcli.dev/fw"
)

// Config is the file sink configuration, section "logfile". Path has no
// default: an enabled file sink without an explicit path is a startup
// error — guessing one would be worse.
type Config struct {
	Version uint32 `json:"version"`
	Path    string `json:"path" conf:"logfile-path" usage:"log file path (required when the sink is enabled)"`
	Level   string `json:"level" conf:"logfile-level" usage:"minimum level logged to the file (debug, info, warn, error)"`
	Format  string `json:"format" conf:"logfile-format" usage:"file log format: text or json"`
	Mode    string `json:"mode" conf:"logfile-mode" usage:"octal permissions for a newly created log file"`
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

var _ fw.Starter = (*File)(nil)
var _ fw.Configurable = (*File)(nil)
var _ slog.Handler = (*File)(nil)
