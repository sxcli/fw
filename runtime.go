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

	"sxcli.dev/conf/engine"
	"sxcli.dev/conf/fail"
	"sxcli.dev/fw/internal/registry"
)

// catalog is the composition's data plane: the registry, the
// operator-name index and the suppressed set — one binary's cataloged
// truth, together with the operations that belong to that data. The
// runtime EMBEDS the live one; the system service receives a snapshot
// copy at attach, so an introspection view holds exactly the data it
// needs and nothing else — no environment, no sources, no seams.
type catalog struct {
	reg        *registry.Registry
	byAlias    map[string]*registry.Descriptor // every operator name → its service; built by index
	suppressed []string
}

// index builds the operator-name index: every alias resolves to its
// service. Composed-alias collisions were Build violations — a clash
// here is a framework bug, reported not swallowed.
func (ca *catalog) index(c *fail.Collector) {
	ca.byAlias = map[string]*registry.Descriptor{}
	for _, d := range ca.reg.All() {
		for _, a := range d.Aliases {
			if prev, taken := ca.byAlias[a]; taken && prev != d {
				c.Fail("operator name %q resolves to both %q and %q", a, prev.ID, d.ID)
			} else {
				ca.byAlias[a] = d
			}
		}
	}
}

// snapshot returns an independent copy of the catalog's data — the
// world an introspection view answers from. Descriptors are shared
// (data-plane); membership, index and suppressed set are copied, so
// ejection and any later mutation of the live catalog cannot reach
// the copy.
func (ca *catalog) snapshot() *catalog {
	aliases := make(map[string]*registry.Descriptor, len(ca.byAlias))
	for a, d := range ca.byAlias {
		aliases[a] = d
	}
	return &catalog{
		reg:        ca.reg.Snapshot(),
		byAlias:    aliases,
		suppressed: append([]string(nil), ca.suppressed...),
	}
}

// runtime carries every external dependency of one run, injectable for
// hermetic tests. Main builds the production one from the package
// globals and the platform layer.
type runtime struct {
	catalog
	c              *fail.Collector
	argv           []string
	lookupEnv      func(string) (string, bool)
	stdout         io.Writer
	stderr         io.Writer
	locations      func(appletID string) []engine.Location
	stat           func(string) (int64, error)
	lstat          func(string) error
	open           func(string) (io.ReadCloser, error)
	openPinned     func(string) (io.ReadCloser, error)
	maxConfigBytes int64            // config file size cap in bytes; <=0 → the 1 MiB default
	execApplet     func(Applet) int // nil → applet.Run(); the SCM handler overrides
	reported       bool
	translatorID   string // id of the sole Translator-providing service, "" = none
}

func productionRuntime(app *App, argv []string, execApplet func(Applet) int) *runtime {
	return &runtime{
		catalog:   catalog{reg: app.reg, suppressed: suppressedCore},
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
		open:           func(path string) (io.ReadCloser, error) { return os.Open(path) },
		openPinned:     engine.OpenPinned,
		maxConfigBytes: maxConfigSize,
		execApplet:     execApplet,
	}
}
