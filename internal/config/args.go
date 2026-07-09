package config

import (
	"fmt"
	"reflect"
	"strings"
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
func (s *Schema) parseArgs(args []string, lenient bool) ([]string, []error) {
	p := &argParser{schema: s, lenient: lenient, reset: map[*Field]bool{}}
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
	return p.pending, p.errs
}

type argParser struct {
	schema  *Schema
	lenient bool
	pending []string        // bare tokens; positionals only if nothing follows
	reset   map[*Field]bool // slice fields already reset by this parse
	errs    []error
}

func (p *argParser) fail(format string, args ...any) {
	p.errs = append(p.errs, fmt.Errorf(format, args...))
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
	known := shorts != ""
	for _, c := range shorts {
		_, ok := p.schema.short[string(c)]
		known = known && ok
	}
	if known {
		p.flagSeen("-" + shorts)
		for i, c := range shorts {
			f := p.schema.short[string(c)]
			last := i == len(shorts)-1
			if !last && !isBool(f) {
				p.fail("-%s: only the last flag of a bundle may take a value, -%c does not take one here", shorts, c)
			} else if isBool(f) {
				v := "true"
				if last && hasValue {
					v = value
				}
				p.set(f, "-"+string(c), v)
			} else if hasValue {
				p.set(f, "-"+string(c), value)
			} else if len(rest) > 0 {
				p.set(f, "-"+string(c), rest[0])
				extra = 1
			} else {
				p.fail("-%s: missing value", string(c))
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
// repetitions append.
func (p *argParser) set(f *Field, display, value string) {
	target := p.schema.owner[f].cfg.Elem().FieldByIndex(f.Path)
	var err error
	if f.IsSlice {
		if !p.reset[f] {
			p.reset[f] = true
			target.Set(reflect.MakeSlice(target.Type(), 0, 4))
		}
		err = appendFromString(target, value)
	} else {
		err = setFromString(target, value)
	}
	if err != nil {
		p.fail("%s: %v", display, err)
	}
}

func isBool(f *Field) bool {
	return !f.IsSlice && f.Type.Kind() == reflect.Bool
}
