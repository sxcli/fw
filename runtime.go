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

package fw

import (
	"io"
	"os"

	"sxcli.dev/fw/conf/engine"
	"sxcli.dev/fw/internal/fail"
	"sxcli.dev/fw/internal/registry"
)

// runtime carries every external dependency of one run, injectable for
// hermetic tests. Main builds the production one from the package
// globals and the platform layer.
type runtime struct {
	reg          *registry.Registry
	c            *fail.Collector
	argv         []string
	lookupEnv    func(string) (string, bool)
	stdout       io.Writer
	stderr       io.Writer
	locations    func(appletID string) []engine.Location
	stat         func(string) (int64, error)
	lstat        func(string) error
	open         func(string) (io.ReadCloser, error)
	openPinned   func(string) (io.ReadCloser, error)
	suppressed   []string
	maxConfig    int64            // config file size cap; <=0 → the 1 MiB default
	execApplet   func(Applet) int // nil → applet.Run(); the SCM handler overrides
	reported     bool
	translatorID string                          // id of the sole Provides[Translator] service, "" = none
	byAlias      map[string]*registry.Descriptor // every operator name → its service; built by run()
}

func productionRuntime(app *App, argv []string, execApplet func(Applet) int) *runtime {
	return &runtime{
		reg:       app.reg,
		c:         &fail.Collector{},
		argv:      argv,
		lookupEnv: os.LookupEnv,
		stdout:    os.Stdout,
		stderr:    os.Stderr,
		locations: engine.ProductionLocations,
		stat:      engine.StatRegular,
		lstat: func(path string) error {
			_, err := os.Lstat(path)
			return err
		},
		open:       func(path string) (io.ReadCloser, error) { return os.Open(path) },
		openPinned: engine.OpenPinned,
		suppressed: suppressedCore,
		maxConfig:  maxConfigSize,
		execApplet: execApplet,
	}
}
