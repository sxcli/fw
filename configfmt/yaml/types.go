// Package yaml provides the YAML config format provider: it transcodes
// .yaml/.yml configuration files to and from the core's native JSON.
// The provider is stateless — pure stream transforms per the
// ConfigFormatProvider contract — and is pulled into the closure only
// when a yaml config file actually matched:
//
//	import _ "sxcli.dev/fw/configfmt/yaml"
package yaml

import sxclifw "sxcli.dev/fw"

// YAML is the provider service.
type YAML struct{}

var _ sxclifw.ConfigFormatProvider = (*YAML)(nil)
