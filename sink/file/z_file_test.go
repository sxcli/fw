package file

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func configured(t *testing.T, cfg Config) *File {
	t.Helper()
	s := &File{cfg: cfg}
	if err := s.Configured(); err != nil {
		t.Fatalf("Configured failed: %v", err)
	}
	t.Cleanup(func() { s.Stop() })
	return s
}

func TestWritesAndGates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "box.log")
	s := configured(t, Config{Path: path, Level: "info", Format: "text", Mode: "0600"})
	logger := slog.New(s)
	logger.Debug("hidden")
	logger.Info("shown", "key", "value")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if strings.Contains(text, "hidden") || !strings.Contains(text, "msg=shown") || !strings.Contains(text, "key=value") {
		t.Errorf("file content wrong:\n%s", text)
	}
}

func TestAppendsAcrossRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "box.log")
	first := configured(t, Config{Path: path, Level: "info", Format: "text", Mode: "0600"})
	slog.New(first).Info("first run")
	if err := first.Stop(); err != nil {
		t.Fatal(err)
	}
	second := configured(t, Config{Path: path, Level: "info", Format: "text", Mode: "0600"})
	slog.New(second).Info("second run")
	content, _ := os.ReadFile(path)
	if !strings.Contains(string(content), "first run") || !strings.Contains(string(content), "second run") {
		t.Errorf("file must append across runs:\n%s", content)
	}
}

func TestJSONFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "box.log")
	s := configured(t, Config{Path: path, Level: "info", Format: "json", Mode: "0600"})
	slog.New(s).Info("hello")
	content, _ := os.ReadFile(path)
	if !strings.Contains(string(content), `"msg":"hello"`) {
		t.Errorf("json output wrong:\n%s", content)
	}
}

func TestModeApplied(t *testing.T) {
	path := filepath.Join(t.TempDir(), "box.log")
	configured(t, Config{Path: path, Level: "info", Format: "text", Mode: "0600"})
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// umask may clear bits but never add any
	if fi.Mode().Perm()&^os.FileMode(0o600) != 0 {
		t.Errorf("file has permissions beyond requested 0600: %v", fi.Mode().Perm())
	}
}

func TestConfiguredRejections(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		cfg  Config
	}{
		{"missing path", Config{Level: "info", Format: "text", Mode: "0600"}},
		{"bad level", Config{Path: filepath.Join(dir, "a.log"), Level: "loud", Format: "text", Mode: "0600"}},
		{"bad format", Config{Path: filepath.Join(dir, "b.log"), Level: "info", Format: "xml", Mode: "0600"}},
		{"bad mode", Config{Path: filepath.Join(dir, "c.log"), Level: "info", Format: "text", Mode: "rw-r"}},
		{"unopenable path", Config{Path: filepath.Join(dir, "no", "such", "dir", "d.log"), Level: "info", Format: "text", Mode: "0600"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &File{cfg: tc.cfg}
			if err := s.Configured(); err == nil {
				s.Stop()
				t.Error("expected a Configured error")
			}
		})
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("a rejected config must never create a file: %v", entries)
	}
}

func TestValidationBeforeOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "never.log")
	s := &File{cfg: Config{Path: path, Level: "bogus", Format: "text", Mode: "0600"}}
	if err := s.Configured(); err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file must not exist after a rejected config")
	}
}

func TestInertBeforeConfiguredAndAfterStop(t *testing.T) {
	s := &File{}
	ctx := context.Background()
	if s.Enabled(ctx, slog.LevelError) {
		t.Error("unconfigured sink must accept nothing")
	}
	path := filepath.Join(t.TempDir(), "box.log")
	s = configured(t, Config{Path: path, Level: "info", Format: "text", Mode: "0600"})
	if s.Start() != nil {
		t.Error("Start must be a no-op")
	}
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
	if s.Enabled(ctx, slog.LevelError) {
		t.Error("stopped sink must accept nothing")
	}
	if err := s.Stop(); err != nil {
		t.Error("double Stop must be safe")
	}
}
