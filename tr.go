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

package fw

import (
	"fmt"
	"strings"
)

// activeTranslator is the Translator chosen and configured by the
// pipeline (spec §7); nil means raw msgids. It is set during the
// translator-first Configured pass — before any other service's
// Configured and before anything renders — and never mutated after,
// so goroutines started later inherit the happens-before edge from
// the sequential lifecycle.
var activeTranslator Translator

// translate looks the msgid up in the active translator; a miss — or
// no translator — returns the msgid itself, gettext-style.
func translate(msgid string) string {
	out := msgid
	if activeTranslator != nil {
		if s, ok := activeTranslator.Translate(msgid); ok {
			out = s
		}
	}
	return out
}

// Tr translates and formats a user-facing message. The format string is
// the gettext msgid: translation is translate-then-format — the format
// is looked up in the registered Translator (when one is configured)
// and the placeholders are substituted into the translation. Without a
// translator the lookup is the identity — Tr is pure formatting — so
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
	return trFormat(translate(format), args...)
}

// TrN translates and formats a message whose shape depends on a
// quantity: msgid is the singular form, msgidPlural the plural, both
// English (msgids are the default text). The registered Translator's
// catalog formula picks the target language's form — Bulgarian its
// count form, Russian its three, Arabic its six; without a translator,
// on a lookup miss, or when translation is degraded, the English rule
// (n != 1) picks between the two msgids.
//
// The placeholder {n} is implicitly bound to n — the name "n" is
// reserved in TrN formats, and a caller-supplied "n" pair is shadowed.
// Other args are name/value pairs exactly as in Tr.
//
//	TrN("{n} window closed", "{n} windows closed", k)
func TrN(msgid, msgidPlural string, n int, args ...any) string {
	format := ""
	found := false
	if activeTranslator != nil {
		if s, ok := activeTranslator.TranslateN(msgid, msgidPlural, n); ok {
			format, found = s, true
		}
	}
	if !found {
		if n != 1 {
			format = msgidPlural
		} else {
			format = msgid
		}
	}
	// appended last: later pairs win in trFormat, so the implicit
	// binding shadows any caller-supplied "n"
	return trFormat(format, append(args, "n", n)...)
}

// trFormat is the placeholder scanner shared by Tr and TrN; format is
// already translated.
func trFormat(format string, args ...any) string {
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
