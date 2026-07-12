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
	"bytes"
	"fmt"
	"io"

	goyaml "github.com/goccy/go-yaml"

	sxclifw "sxcli.dev/fw"
)

func init() {
	sxclifw.Register("yaml", &YAML{},
		sxclifw.Provides[sxclifw.ConfigFormatProvider](),
		sxclifw.WithMetadata(&sxclifw.Metadata{
			Description: "YAML config format provider: transcodes .yaml/.yml configuration files to and from the core's native JSON",
		}),
	)
}

// Extensions returns the file extensions this provider transcodes.
func (p *YAML) Extensions() []string {
	return []string{"yaml", "yml"}
}

// ToJSON converts one yaml document to json. Config files are small, so
// the stream is slurped.
func (p *YAML) ToJSON(in io.Reader) (io.Reader, error) {
	var out io.Reader
	raw, err := io.ReadAll(in)
	if err == nil {
		var converted []byte
		if converted, err = goyaml.YAMLToJSON(raw); err == nil {
			out = bytes.NewReader(converted)
		} else {
			err = fmt.Errorf("yaml: %v", err)
		}
	}
	return out, err
}

// FromJSON converts json back to yaml, serving --write-config.
func (p *YAML) FromJSON(in io.Reader) (io.Reader, error) {
	var out io.Reader
	raw, err := io.ReadAll(in)
	if err == nil {
		var converted []byte
		if converted, err = goyaml.JSONToYAML(raw); err == nil {
			out = bytes.NewReader(converted)
		} else {
			err = fmt.Errorf("yaml: %v", err)
		}
	}
	return out, err
}
