package yaml

import (
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"
)

func toJSON(t *testing.T, doc string) string {
	t.Helper()
	p := &YAML{}
	out, err := p.ToJSON(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}
	raw, _ := io.ReadAll(out)
	return string(raw)
}

func TestExtensions(t *testing.T) {
	if !reflect.DeepEqual((&YAML{}).Extensions(), []string{"yaml", "yml"}) {
		t.Errorf("extensions wrong: %v", (&YAML{}).Extensions())
	}
}

func TestToJSON(t *testing.T) {
	doc := `
logfile:
  path: /var/log/box.log
  level: debug
  backups: 3
  tags:
    - a
    - b
core:
  disable: [sqlite]
  verbose: true
`
	var got map[string]map[string]any
	if err := json.Unmarshal([]byte(toJSON(t, doc)), &got); err != nil {
		t.Fatalf("output is not json: %v", err)
	}
	if got["logfile"]["path"] != "/var/log/box.log" || got["logfile"]["level"] != "debug" {
		t.Errorf("strings wrong: %v", got)
	}
	if got["logfile"]["backups"] != float64(3) || got["core"]["verbose"] != true {
		t.Errorf("scalars wrong: %v", got)
	}
	if !reflect.DeepEqual(got["logfile"]["tags"], []any{"a", "b"}) {
		t.Errorf("arrays wrong: %v", got)
	}
	if !reflect.DeepEqual(got["core"]["disable"], []any{"sqlite"}) {
		t.Errorf("flow arrays wrong: %v", got)
	}
}

func TestRoundTrip(t *testing.T) {
	p := &YAML{}
	original := `{"console": {"level": "warn", "output": "stdout"}, "core": {"enable": ["syslog"]}}`
	back, err := p.FromJSON(strings.NewReader(original))
	if err != nil {
		t.Fatalf("FromJSON failed: %v", err)
	}
	yamlText, _ := io.ReadAll(back)
	if !strings.Contains(string(yamlText), "level: warn") {
		t.Errorf("yaml output wrong:\n%s", yamlText)
	}
	again, err := p.ToJSON(strings.NewReader(string(yamlText)))
	if err != nil {
		t.Fatalf("ToJSON of round trip failed: %v", err)
	}
	var a, b map[string]any
	raw, _ := io.ReadAll(again)
	json.Unmarshal([]byte(original), &a)
	json.Unmarshal(raw, &b)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("round trip diverged:\n%v\n%v", a, b)
	}
}

func TestInvalidYAMLFails(t *testing.T) {
	p := &YAML{}
	if _, err := p.ToJSON(strings.NewReader("a: [unclosed")); err == nil {
		t.Error("invalid yaml must be an error")
	}
}

func TestStatelessPureTransform(t *testing.T) {
	// the contract: usable with zero configuration or lifecycle — the
	// zero value works
	var p YAML
	if _, err := p.ToJSON(strings.NewReader("a: 1")); err != nil {
		t.Errorf("zero-value provider must work: %v", err)
	}
}
