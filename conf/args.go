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

package conf

import (
	"reflect"
	"strings"

	"sxcli.dev/fw/internal/fail"
)

// parseArgs applies the argument source to the schema's config structs.
// In strict mode unknown arguments and misplaced bare tokens are errors
// and trailing bare tokens are returned as positionals. In lenient mode
// — the first pipeline pass, where only the core schema exists — every
// unknown token is skipped without consuming its neighbour, so the
// self-contained --name=value form is the reliable way to pass core
// values whose neighbours are unknown.
//
// A literal "--" ends flag parsing; everything after it is positional.
func (s *Schema) parseArgs(c *fail.Collector, args []string, lenient bool) []string {
	p := &argParser{schema: s, c: c, lenient: lenient, reset: map[*Field]bool{}}
	i := 0
	for i < len(args) {
		token := args[i]
		consumed := 1
		if token == "--" {
			p.pending = append(p.pending, args[i+1:]...)
			consumed = len(args) - i
		} else if strings.HasPrefix(token, "--") {
			consumed += p.long(token[2:], args[i+1:])
		} else if strings.HasPrefix(token, "-") && len(token) > 1 {
			consumed += p.bundle(token[1:], args[i+1:])
		} else {
			p.pending = append(p.pending, token)
		}
		i += consumed
	}
	return p.pending
}

type argParser struct {
	schema  *Schema
	c       *fail.Collector
	lenient bool
	pending []string        // bare tokens; positionals only if nothing follows
	reset   map[*Field]bool // slice fields already reset by this parse
}

func (p *argParser) fail(format string, args ...any) {
	p.c.Fail(format, args...)
}

// flagSeen enforces that bare tokens only trail: hitting a flag while
// bare tokens are pending is an error in strict mode and silently drops
// them in lenient mode.
func (p *argParser) flagSeen(display string) {
	if len(p.pending) > 0 {
		if !p.lenient {
			p.fail("unexpected arguments %q before %s: positionals must come last", p.pending, display)
		}
		p.pending = nil
	}
}

// long handles one --name[=value] token; rest is the remaining argument
// vector. It returns how many extra tokens were consumed.
func (p *argParser) long(body string, rest []string) int {
	extra := 0
	name, value, hasValue := strings.Cut(body, "=")
	if f, known := p.schema.long[name]; known {
		p.flagSeen("--" + name)
		if isBool(f) {
			if !hasValue {
				value = "true"
			}
			p.set(f, "--"+name, value)
		} else if hasValue {
			p.set(f, "--"+name, value)
		} else if len(rest) > 0 {
			p.set(f, "--"+name, rest[0])
			extra = 1
		} else {
			p.fail("--%s: missing value", name)
		}
	} else if !p.lenient {
		p.flagSeen("--" + name)
		p.fail("unknown argument --%s", name)
	}
	return extra
}

// bundle handles one -abc[=value] token: every bundled short must be a
// bool except the last, which may take a value. In lenient mode a bundle
// containing any unknown short is skipped whole, consuming nothing.
func (p *argParser) bundle(body string, rest []string) int {
	extra := 0
	shorts, value, hasValue := strings.Cut(body, "=")
	// byte iteration is deliberate: valid shorts are single ascii
	// characters, so any multi-byte rune can only be unknown anyway
	known := shorts != ""
	for i := 0; i < len(shorts); i++ {
		_, ok := p.schema.short[string(shorts[i])]
		known = known && ok
	}
	if known {
		p.flagSeen("-" + shorts)
		for i := 0; i < len(shorts); i++ {
			c := string(shorts[i])
			f := p.schema.short[c]
			last := i == len(shorts)-1
			if !last && !isBool(f) {
				p.fail("-%s: only the last flag of a bundle may take a value, -%s does not take one here", shorts, c)
			} else if isBool(f) {
				v := "true"
				if last && hasValue {
					v = value
				}
				p.set(f, "-"+c, v)
			} else if hasValue {
				p.set(f, "-"+c, value)
			} else if len(rest) > 0 {
				p.set(f, "-"+c, rest[0])
				extra = 1
			} else {
				p.fail("-%s: missing value", c)
			}
		}
	} else if !p.lenient {
		p.flagSeen("-" + shorts)
		p.fail("unknown argument -%s", shorts)
	}
	return extra
}

// set writes one argument value into its field. The first argument
// occurrence of a slice field replaces any file/env-sourced content;
// repetitions append. Values outside a declared domain are violations.
func (p *argParser) set(f *Field, display, value string) {
	target := f.root.Elem().FieldByIndex(f.Path)
	var err error
	if f.IsSlice {
		if !p.reset[f] {
			p.reset[f] = true
			target.Set(reflect.MakeSlice(target.Type(), 0, 4))
		}
		err = appendFromString(target, value)
		if err == nil && len(f.Allowed) > 0 && !domainHas(f, target.Index(target.Len()-1)) {
			p.fail("%s: value %v is not among the allowed values %v", display, target.Index(target.Len()-1).Interface(), f.Allowed)
		}
	} else {
		err = setFromString(target, value)
		if err == nil {
			checkDomain(p.c, display, f, target)
		}
	}
	if err != nil {
		p.fail("%s: %v", display, err)
	}
}

func isBool(f *Field) bool {
	return !f.IsSlice && f.Type.Kind() == reflect.Bool
}
