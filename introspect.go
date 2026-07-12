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

package sxclifw

// introspectionID is the reserved service id of the core's Introspector.
const introspectionID = "introspection"

// Introspector is the core's read-only view of the binary's
// composition, for services that implement completions, documentation
// generators and similar meta features outside the core. There is
// exactly one: the core constructs and registers it itself under the
// reserved id "introspection" — it reports the composition truth, and
// truth does not federate. Consumers inject it by concrete type:
//
//	type CompletionApplet struct {
//		I *sxclifw.Introspector `inject:""`
//	}
//
// The one price of introspection: a closure containing the Introspector
// is never ejected — enumerating the binary requires keeping the
// registry alive. Only invocations that injected it pay that.
type Introspector struct {
	rt *runtime
}

// Applets returns the ids of every registered applet, in registration
// order.
func (i *Introspector) Applets() []string {
	var out []string
	for _, d := range i.rt.reg.All() {
		if _, isApplet := d.Instance.(Applet); isApplet {
			out = append(out, d.ID)
		}
	}
	return out
}

// Services returns the ids of every registered service — applets
// included — in registration order.
func (i *Introspector) Services() []string {
	var out []string
	for _, d := range i.rt.reg.All() {
		out = append(out, d.ID)
	}
	return out
}

// ConfigExtensions returns every config file extension this binary can
// read: "json" first, then each registered format provider's
// extensions in registration order, deduplicated.
func (i *Introspector) ConfigExtensions() []string {
	out := []string{"json"}
	for _, d := range i.rt.reg.All() {
		if providesType(d, providerType) {
			if p, ok := d.Instance.(ConfigFormatProvider); ok {
				for _, ext := range p.Extensions() {
					if !contains(out, ext) {
						out = append(out, ext)
					}
				}
			}
		}
	}
	return out
}
