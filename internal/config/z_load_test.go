package config

import (
	"io"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"time"
)

// identityYAML pretends to be a YAML provider; ToJSON passes the content
// through, which is enough to prove the transcode wiring and Used
// recording.
type identityYAML struct{}

func (p *identityYAML) Extensions() []string                     { return []string{"yml", "yaml"} }
func (p *identityYAML) ToJSON(in io.Reader) (io.Reader, error)   { return in, nil }
func (p *identityYAML) FromJSON(in io.Reader) (io.Reader, error) { return in, nil }

func fakeFS(files map[string]string) func(string) (io.ReadCloser, error) {
	return func(path string) (io.ReadCloser, error) {
		var r io.ReadCloser
		err := fs.ErrNotExist
		if content, ok := files[path]; ok {
			r = io.NopCloser(strings.NewReader(content))
			err = nil
		}
		return r, err
	}
}

func env(vars map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := vars[name]
		return v, ok
	}
}

type loadSink struct {
	Path     string        `json:"path" arg:"log-path"`
	MaxAge   time.Duration `json:"maxAge" arg:"log-max-age"`
	Backups  int           `json:"backups"`
	Tags     []string      `json:"tags" arg:"tag,t"`
	Rotation struct {
		Size int64 `json:"size"`
	} `json:"rotation"`
}

func TestPrecedenceEndToEnd(t *testing.T) {
	cfg := &loadSink{Path: "default.log", Backups: 1}
	cfg.Rotation.Size = 10
	src := Sources{
		Args:      []string{"--log-max-age", "2h", "trail"},
		LookupEnv: env(map[string]string{"CAT_LOG_PATH": "env.log", "CAT_LOG_MAX_AGE": "1h"}),
		Locations: []string{"/bin/cat-config", "/etc/cat/config", "/home/u/cat/config"},
		Open: fakeFS(map[string]string{
			"/bin/cat-config.json":  `{"filesink": {"path": "bin.log", "maxAge": "5m", "backups": 3, "tags": ["f1","f2"], "rotation": {"size": 99}}}`,
			"/etc/cat/config.yml":   `{"filesink": {"path": "etc.log"}, "coldsvc": {"whatever": 1}}`,
			"/home/u/cat/config.md": `not a config`,
		}),
		Providers: []Provider{&identityYAML{}},
	}
	files, ferrs := LoadFiles(src, "")
	if len(ferrs) != 0 {
		t.Fatalf("unexpected file errors: %v", ferrs)
	}
	if len(files.Used) != 1 {
		t.Errorf("yaml provider use not recorded: %v", files.Used)
	}
	s := newTestSchema(t, &Core{}, map[string]any{"filesink": cfg})
	loaded, errs := s.Apply(files, src)
	if len(errs) != 0 {
		t.Fatalf("unexpected apply errors: %v", errs)
	}
	if cfg.Path != "env.log" {
		t.Errorf("env must beat files: %q", cfg.Path)
	}
	if cfg.MaxAge != 2*time.Hour {
		t.Errorf("args must beat env: %v", cfg.MaxAge)
	}
	if cfg.Backups != 3 || cfg.Rotation.Size != 99 {
		t.Errorf("file-only values not applied: %+v", cfg)
	}
	if !reflect.DeepEqual(cfg.Tags, []string{"f1", "f2"}) {
		t.Errorf("file slice not applied: %v", cfg.Tags)
	}
	if !reflect.DeepEqual(loaded.Positionals, []string{"trail"}) {
		t.Errorf("positionals wrong: %v", loaded.Positionals)
	}
}

func TestLaterLocationOverridesEarlier(t *testing.T) {
	cfg := &loadSink{}
	src := Sources{
		Locations: []string{"/bin/cat-config", "/etc/cat/config"},
		Open: fakeFS(map[string]string{
			"/bin/cat-config.json": `{"filesink": {"path": "bin.log", "backups": 3}}`,
			"/etc/cat/config.json": `{"filesink": {"path": "etc.log"}}`,
		}),
	}
	files, ferrs := LoadFiles(src, "")
	if len(ferrs) != 0 {
		t.Fatalf("unexpected file errors: %v", ferrs)
	}
	s := newTestSchema(t, &Core{}, map[string]any{"filesink": cfg})
	if _, errs := s.Apply(files, src); len(errs) != 0 {
		t.Fatalf("unexpected apply errors: %v", errs)
	}
	if cfg.Path != "etc.log" || cfg.Backups != 3 {
		t.Errorf("later file must override field-wise only: %+v", cfg)
	}
}

func TestExplicitConfigReplacesSearch(t *testing.T) {
	cfg := &loadSink{}
	src := Sources{
		Locations: []string{"/etc/cat/config"},
		Open: fakeFS(map[string]string{
			"/etc/cat/config.json": `{"filesink": {"path": "etc.log"}}`,
			"/tmp/mine.yml":        `{"filesink": {"path": "mine.log"}}`,
		}),
		Providers: []Provider{&identityYAML{}},
	}
	files, ferrs := LoadFiles(src, "/tmp/mine.yml")
	if len(ferrs) != 0 {
		t.Fatalf("unexpected file errors: %v", ferrs)
	}
	s := newTestSchema(t, &Core{}, map[string]any{"filesink": cfg})
	if _, errs := s.Apply(files, src); len(errs) != 0 {
		t.Fatalf("unexpected apply errors: %v", errs)
	}
	if cfg.Path != "mine.log" {
		t.Errorf("--config must replace the search: %q", cfg.Path)
	}
}

func TestFileErrors(t *testing.T) {
	cases := []struct {
		name     string
		explicit string
		files    map[string]string
	}{
		{"explicit missing", "/none.json", map[string]string{}},
		{"unknown extension", "/c.toml", map[string]string{"/c.toml": `{}`}},
		{"ambiguous location", "", map[string]string{
			"/etc/cat/config.json": `{}`,
			"/etc/cat/config.yml":  `{}`,
		}},
		{"broken json", "", map[string]string{"/etc/cat/config.json": `{"x":`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := Sources{
				Locations: []string{"/etc/cat/config"},
				Open:      fakeFS(tc.files),
				Providers: []Provider{&identityYAML{}},
			}
			if _, errs := LoadFiles(src, tc.explicit); len(errs) == 0 {
				t.Error("expected file errors, got none")
			}
		})
	}
}

func TestUnknownKeyInOwnedSection(t *testing.T) {
	cfg := &loadSink{}
	src := Sources{
		Locations: []string{"/etc/cat/config"},
		Open:      fakeFS(map[string]string{"/etc/cat/config.json": `{"filesink": {"pth": "typo.log"}}`}),
	}
	files, ferrs := LoadFiles(src, "")
	if len(ferrs) != 0 {
		t.Fatalf("unexpected file errors: %v", ferrs)
	}
	s := newTestSchema(t, &Core{}, map[string]any{"filesink": cfg})
	if _, errs := s.Apply(files, src); len(errs) == 0 {
		t.Error("unknown key in an owned section must be an error")
	}
}

func TestApplyCoreSeesFileValues(t *testing.T) {
	src := Sources{
		Args:      []string{"--enable", "mysql"},
		LookupEnv: env(map[string]string{"CAT_DISABLE": "sqlite,boltdb"}),
		Locations: []string{"/etc/cat/config"},
		Open:      fakeFS(map[string]string{"/etc/cat/config.json": `{"core": {"disable": ["never"], "override": ["sqlite=mysql"]}}`}),
	}
	files, ferrs := LoadFiles(src, "")
	if len(ferrs) != 0 {
		t.Fatalf("unexpected file errors: %v", ferrs)
	}
	core, errs := files.ApplyCore("cat", src)
	if len(errs) != 0 {
		t.Fatalf("unexpected core errors: %v", errs)
	}
	if !reflect.DeepEqual(core.Disable, []string{"sqlite", "boltdb"}) {
		t.Errorf("env must beat file for disable: %v", core.Disable)
	}
	if !reflect.DeepEqual(core.Override, []string{"sqlite=mysql"}) {
		t.Errorf("file-sourced override lost: %v", core.Override)
	}
	if !reflect.DeepEqual(core.Enable, []string{"mysql"}) {
		t.Errorf("arg-sourced enable lost: %v", core.Enable)
	}
}

func TestPeekCore(t *testing.T) {
	core, errs := PeekCore("cat", Sources{
		Args:      []string{"--unknown-service-flag", "junk", "--config=x.yml", "--write-config"},
		LookupEnv: env(map[string]string{"CAT_HELP": "true"}),
	})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if core.Config != "x.yml" || !core.WriteConfig || !core.Help {
		t.Errorf("core values not extracted: %+v", core)
	}
}

func TestMarshalIndentRoundTrip(t *testing.T) {
	cfg := &loadSink{Path: "a.log", MaxAge: 90 * time.Minute, Tags: []string{"x"}}
	cfg.Rotation.Size = 5
	s := newTestSchema(t, &Core{Config: "c.json"}, map[string]any{"filesink": cfg})
	out, err := s.MarshalIndent()
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	text := string(out)
	for _, want := range []string{`"maxAge": "1h30m0s"`, `"path": "a.log"`, `"size": 5`, `"config": "c.json"`, `"rotation"`} {
		if !strings.Contains(text, want) {
			t.Errorf("dump missing %s:\n%s", want, text)
		}
	}
}
