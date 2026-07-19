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

package yaml

import (
	"sxcli.dev/fw"
)

// ID is the yaml provider's identity; operators call it "yaml".
const ID = "sxcli.dev/fw/configfmt/yaml"

func init() {
	fw.NewBareRegistration(ID, func() *YAML { return &YAML{} }).
		Alias("yaml").
		Provides(fw.Iface[fw.ConfigFormatProvider]()).
		Metadata(&fw.Metadata{
			Description: "YAML config format provider: transcodes .yaml/.yml configuration files to and from the core's native JSON",
		}).
		Register()
}
