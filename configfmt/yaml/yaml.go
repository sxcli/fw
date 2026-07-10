package yaml

import (
	"bytes"
	"fmt"
	"io"

	goyaml "github.com/goccy/go-yaml"

	sxclifw "github.com/sxcli/sxcli-fw"
)

func init() {
	sxclifw.Register("yaml", &YAML{},
		sxclifw.Provides[sxclifw.ConfigFormatProvider](),
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
