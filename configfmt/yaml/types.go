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

// Package yaml registers the conf module's YAML provider as a
// composition service: the bare transcoder lives in sxcli.dev/conf/
// yaml for standalone use; this package is the framework's service
// wrapper — pulled into the closure only when a yaml config file
// actually matched.
package yaml

import (
	confyaml "sxcli.dev/conf/yaml"

	"sxcli.dev/fw"
)

// YAML is the provider service: the conf module's transcoder,
// registered into the composition.
type YAML struct{ confyaml.YAML }

var _ fw.ConfigFormatProvider = (*YAML)(nil)
