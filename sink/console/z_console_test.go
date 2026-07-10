package console

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func configured(t *testing.T, cfg Config, out *bytes.Buffer) *Console {
	t.Helper()
	s := &Console{cfg: cfg}
	if err := s.build(out); err != nil {
		t.Fatalf("build failed: %v", err)
	}
	return s
}

func TestTextOutputAndLevelGating(t *testing.T) {
	var out bytes.Buffer
	s := configured(t, Config{Level: "info", Format: "text"}, &out)
	logger := slog.New(s)
	logger.Debug("hidden")
	logger.Info("shown", "key", "value")
	text := out.String()
	if strings.Contains(text, "hidden") {
		t.Error("debug must be gated at info level")
	}
	if !strings.Contains(text, "msg=shown") || !strings.Contains(text, "key=value") {
		t.Errorf("info record wrong:\n%s", text)
	}
}

func TestDebugLevelPassesDebug(t *testing.T) {
	var out bytes.Buffer
	s := configured(t, Config{Level: "debug", Format: "text"}, &out)
	slog.New(s).Debug("visible")
	if !strings.Contains(out.String(), "msg=visible") {
		t.Errorf("debug record lost:\n%s", out.String())
	}
}

func TestJSONFormat(t *testing.T) {
	var out bytes.Buffer
	s := configured(t, Config{Level: "info", Format: "json"}, &out)
	slog.New(s).Info("hello")
	if !strings.Contains(out.String(), `"msg":"hello"`) {
		t.Errorf("json output wrong:\n%s", out.String())
	}
}

func TestDerivedViewCarriesAttrs(t *testing.T) {
	var out bytes.Buffer
	s := configured(t, Config{Level: "info", Format: "text"}, &out)
	slog.New(s).With("request", "42").WithGroup("db").Info("done", "rows", 3)
	text := out.String()
	if !strings.Contains(text, "request=42") || !strings.Contains(text, "db.rows=3") {
		t.Errorf("derived view chain wrong:\n%s", text)
	}
}

func TestInvalidConfig(t *testing.T) {
	cases := []Config{
		{Level: "loud", Format: "text", Output: "stderr"},
		{Level: "info", Format: "xml", Output: "stderr"},
		{Level: "info", Format: "text", Output: "/dev/null"},
	}
	for _, cfg := range cases {
		s := &Console{cfg: cfg}
		if err := s.Configured(); err == nil {
			t.Errorf("config %+v must be rejected", cfg)
		}
	}
}

func TestUnconfiguredSinkIsInert(t *testing.T) {
	s := &Console{}
	ctx := context.Background()
	if s.Enabled(ctx, slog.LevelError) {
		t.Error("unconfigured sink must accept nothing")
	}
	if err := s.Handle(ctx, slog.Record{Level: slog.LevelError, Message: "x"}); err != nil {
		t.Errorf("unconfigured Handle must be a silent no-op: %v", err)
	}
	if s.WithAttrs([]slog.Attr{slog.String("a", "b")}) != slog.Handler(s) {
		t.Error("unconfigured WithAttrs must return the receiver")
	}
}

func TestLifecycleNoOps(t *testing.T) {
	s := &Console{}
	if s.Start() != nil || s.Stop() != nil {
		t.Error("start/stop must be no-ops")
	}
}
