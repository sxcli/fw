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

// Package yaml provides the YAML config format provider: it transcodes
// .yaml/.yml configuration files to and from the core's native JSON.
// The provider is stateless — pure stream transforms per the
// ConfigFormatProvider contract — and is pulled into the closure only
// when a yaml config file actually matched:
//
//	import _ "sxcli.dev/fw/configfmt/yaml"
package yaml

import "sxcli.dev/fw"

// YAML is the provider service.
type YAML struct{}

var _ fw.ConfigFormatProvider = (*YAML)(nil)
