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

// Solo is the single-applet front door and the registration chain's
// second terminal: commit the registration, accept it (plus whatever
// its closure needs from the catalog — dependencies resolve as
// always), and run. It never returns.
//
//	fw.Solo(fw.NewRegistration("example.com/mytool/srv", newSrv,
//	    func(s *Srv) *Config { return &s.cfg }).
//	    Alias("srv"))
//
// Growing beyond one applet means graduating to the Builder — Solo IS
// the builder route, pre-composed as AcceptAll with the sole applet.
func Solo[T any](r *Registration[T]) {
	r.Register()
	Builder().AcceptAll().Main()
}
