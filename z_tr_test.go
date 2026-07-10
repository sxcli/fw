package sxclifw

import (
	"testing"
	"time"
)

func TestTr(t *testing.T) {
	cases := []struct {
		name   string
		format string
		args   []any
		want   string
	}{
		{"pairs by name", "valueA: {int} and valueB: {bool}", []any{"bool", false, "int", 100}, "valueA: 100 and valueB: false"},
		{"no placeholders", "plain text", nil, "plain text"},
		{"missing name stays verbatim", "hi {who}", nil, "hi {who}"},
		{"escaped braces", "{{literal}} {x}", []any{"x", 1}, "{literal} 1"},
		{"unterminated placeholder", "oops {name", nil, "oops {name"},
		{"repeated placeholder", "{a}+{a}", []any{"a", 2}, "2+2"},
		{"odd trailing arg ignored", "{a} {b}", []any{"a", 1, "b"}, "1 {b}"},
		{"non-string name ignored", "{a}", []any{1, "x", "a", "y"}, "y"},
		{"empty placeholder", "{}", nil, "{}"},
		{"percent-v semantics", "{d}", []any{"d", 90 * time.Minute}, "1h30m0s"},
		{"placeholder hugged by escapes", "{{{a}}}", []any{"a", 5}, "{5}"},
		{"lone closing brace", "a } b", nil, "a } b"},
		{"empty format", "", []any{"a", 1}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Tr(tc.format, tc.args...); got != tc.want {
				t.Errorf("Tr(%q, %v) = %q, want %q", tc.format, tc.args, got, tc.want)
			}
		})
	}
}
