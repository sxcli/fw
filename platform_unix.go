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

//go:build !windows

package fw

import (
	"os"
	"path/filepath"
)

// platformMain runs the pipeline with the process arguments; there is
// no service mode on this platform.
func platformMain(app *App) int {
	return run(productionRuntime(app, os.Args, nil))
}

// systemConfigDir returns the system-wide config location root.
func systemConfigDir() string {
	return "/etc"
}

// binaryBasename extracts the applet-selector name from argv[0].
func binaryBasename(argv0 string) string {
	return filepath.Base(argv0)
}
