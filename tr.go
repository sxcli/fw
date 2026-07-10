package sxclifw

import (
	"fmt"
	"strings"
)

// Tr translates and formats a user-facing message. The format string is
// the gettext msgid: translation is translate-then-format, so when
// catalogs arrive the format is looked up first and the placeholders are
// substituted into the translation. In the current version no catalogs
// exist and the lookup is the identity — Tr is pure formatting — but
// write formats as final, translatable English sentences.
//
// args are name/value pairs resolving the {name} placeholders:
//
//	Tr("valueA: {int} and valueB: {bool}", "bool", false, "int", 100)
//	// → "valueA: 100 and valueB: false"
//
// Values render with fmt's %v semantics. {{ and }} escape literal
// braces. A placeholder with no matching name — and any malformed pair
// (non-string name, trailing odd value) — is left verbatim rather than
// erroring: a visible {name} in the output is a bug you can see and
// grep for.
//
// The placeholder syntax matches gettext's python-brace-format flag, so
// the standard tooling (msgfmt --check, Poedit, Weblate) validates
// placeholders in translations once catalogs exist.
func Tr(format string, args ...any) string {
	var values map[string]any
	for i := 0; i+1 < len(args); i += 2 {
		if name, ok := args[i].(string); ok {
			if values == nil {
				values = map[string]any{}
			}
			values[name] = args[i+1]
		}
	}
	var b strings.Builder
	b.Grow(len(format))
	for i := 0; i < len(format); {
		if format[i] == '{' {
			if i+1 < len(format) && format[i+1] == '{' {
				b.WriteByte('{')
				i += 2
			} else if j := strings.IndexByte(format[i+1:], '}'); j >= 0 {
				if v, present := values[format[i+1:i+1+j]]; present {
					fmt.Fprintf(&b, "%v", v)
				} else {
					b.WriteString(format[i : i+j+2])
				}
				i += j + 2
			} else {
				b.WriteString(format[i:])
				i = len(format)
			}
		} else if format[i] == '}' && i+1 < len(format) && format[i+1] == '}' {
			b.WriteByte('}')
			i += 2
		} else {
			b.WriteByte(format[i])
			i++
		}
	}
	return b.String()
}
