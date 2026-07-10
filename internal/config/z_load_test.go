package config

import (
	"errors"
	"io"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sxcli/sxcli-fw/internal/fail"
)

func mustLoadFiles(t *testing.T, src Sources, explicit string) *Files {
	t.Helper()
	c := &fail.Collector{}
	files := LoadFiles(c, src, explicit)
	if c.Len() != 0 {
		t.Fatalf("unexpected file errors: %v", c.All())
	}
	return files
}

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

func fakeStat(files map[string]string) func(string) (int64, error) {
	return func(path string) (int64, error) {
		var size int64
		err := fs.ErrNotExist
		if content, ok := files[path]; ok {
			size = int64(len(content))
			err = nil
		}
		return size, err
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
	disk := map[string]string{
		"/bin/cat-config.json":  `{"filesink": {"path": "bin.log", "maxAge": "5m", "backups": 3, "tags": ["f1","f2"], "rotation": {"size": 99}}}`,
		"/etc/cat/config.yml":   `{"filesink": {"path": "etc.log"}, "coldsvc": {"whatever": 1}}`,
		"/home/u/cat/config.md": `not a config`,
	}
	src := Sources{
		Args:      []string{"--log-max-age", "2h", "trail"},
		LookupEnv: env(map[string]string{"CAT_LOG_PATH": "env.log", "CAT_LOG_MAX_AGE": "1h"}),
		Locations: []Location{{Base: "/bin/cat-config"}, {Base: "/etc/cat/config"}, {Base: "/home/u/cat/config"}},
		Stat:      fakeStat(disk),
		Open:      fakeFS(disk),
		Providers: []Provider{&identityYAML{}},
	}
	files := mustLoadFiles(t, src, "")
	if len(files.Used) != 1 {
		t.Errorf("yaml provider use not recorded: %v", files.Used)
	}
	s := newTestSchema(t, &Core{}, map[string]any{"filesink": cfg})
	c := &fail.Collector{}
	loaded := s.Apply(c, files, src)
	if c.Len() != 0 {
		t.Fatalf("unexpected apply errors: %v", c.All())
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
	disk := map[string]string{
		"/bin/cat-config.json": `{"filesink": {"path": "bin.log", "backups": 3}}`,
		"/etc/cat/config.json": `{"filesink": {"path": "etc.log"}}`,
	}
	src := Sources{
		Locations: []Location{{Base: "/bin/cat-config"}, {Base: "/etc/cat/config"}},
		Stat:      fakeStat(disk),
		Open:      fakeFS(disk),
	}
	files := mustLoadFiles(t, src, "")
	s := newTestSchema(t, &Core{}, map[string]any{"filesink": cfg})
	c := &fail.Collector{}
	s.Apply(c, files, src)
	if c.Len() != 0 {
		t.Fatalf("unexpected apply errors: %v", c.All())
	}
	if cfg.Path != "etc.log" || cfg.Backups != 3 {
		t.Errorf("later file must override field-wise only: %+v", cfg)
	}
}

func TestExplicitConfigReplacesSearch(t *testing.T) {
	cfg := &loadSink{}
	disk := map[string]string{
		"/etc/cat/config.json": `{"filesink": {"path": "etc.log"}}`,
		"/tmp/mine.yml":        `{"filesink": {"path": "mine.log"}}`,
	}
	src := Sources{
		Locations: []Location{{Base: "/etc/cat/config"}},
		Stat:      fakeStat(disk),
		Open:      fakeFS(disk),
		Providers: []Provider{&identityYAML{}},
	}
	files := mustLoadFiles(t, src, "/tmp/mine.yml")
	s := newTestSchema(t, &Core{}, map[string]any{"filesink": cfg})
	c := &fail.Collector{}
	s.Apply(c, files, src)
	if c.Len() != 0 {
		t.Fatalf("unexpected apply errors: %v", c.All())
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
		{"trailing garbage", "", map[string]string{"/etc/cat/config.json": `{"filesink": {}} {"more": true}`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := Sources{
				Locations: []Location{{Base: "/etc/cat/config"}},
				Stat:      fakeStat(tc.files),
				Open:      fakeFS(tc.files),
				Providers: []Provider{&identityYAML{}},
			}
			c := &fail.Collector{}
			LoadFiles(c, src, tc.explicit)
			if c.Len() == 0 {
				t.Error("expected file errors, got none")
			}
		})
	}
}

// jsonThief claims the native json extension.
type jsonThief struct{}

func (p *jsonThief) Extensions() []string                     { return []string{"json"} }
func (p *jsonThief) ToJSON(in io.Reader) (io.Reader, error)   { return in, nil }
func (p *jsonThief) FromJSON(in io.Reader) (io.Reader, error) { return in, nil }

func TestProviderClaimingJSONFails(t *testing.T) {
	src := Sources{
		Locations: []Location{{Base: "/etc/cat/config"}},
		Stat:      fakeStat(map[string]string{}),
		Open:      fakeFS(map[string]string{}),
		Providers: []Provider{&jsonThief{}},
	}
	c := &fail.Collector{}
	LoadFiles(c, src, "")
	if c.Len() == 0 {
		t.Error("a provider claiming the native json extension must be a violation")
	}
}

func TestDuplicateExtensionClaimFails(t *testing.T) {
	src := Sources{
		Locations: []Location{{Base: "/etc/cat/config"}},
		Stat:      fakeStat(map[string]string{}),
		Open:      fakeFS(map[string]string{}),
		Providers: []Provider{&identityYAML{}, &identityYAML{}},
	}
	c := &fail.Collector{}
	LoadFiles(c, src, "")
	if c.Len() == 0 {
		t.Error("two providers claiming the same extension must be a violation")
	}
}

func TestEmptyEnvValueMeansEmptySlice(t *testing.T) {
	cfg := &loadSink{Tags: []string{"default"}}
	src := Sources{
		LookupEnv: env(map[string]string{"CAT_TAG": ""}),
	}
	s := newTestSchema(t, &Core{}, map[string]any{"filesink": cfg})
	c := &fail.Collector{}
	s.Apply(c, &Files{}, src)
	if c.Len() != 0 {
		t.Fatalf("unexpected errors: %v", c.All())
	}
	if cfg.Tags == nil || len(cfg.Tags) != 0 {
		t.Errorf("empty env value must yield an empty slice, got %#v", cfg.Tags)
	}
}

func TestPinnedLocationUsesPinnedOpener(t *testing.T) {
	cfg := &loadSink{}
	pinnedCalled := false
	disk := map[string]string{"/opt/box/cat-config.json": `{"filesink": {"path": "pinned.log"}}`}
	src := Sources{
		Locations: []Location{{Base: "/opt/box/cat-config", Pinned: true}},
		Stat:      fakeStat(disk),
		Open: func(path string) (io.ReadCloser, error) {
			t.Fatalf("pinned location must never use the plain opener (asked for %q)", path)
			return nil, nil
		},
		OpenPinned: func(path string) (io.ReadCloser, error) {
			pinnedCalled = true
			return fakeFS(disk)(path)
		},
	}
	files := mustLoadFiles(t, src, "")
	if !pinnedCalled {
		t.Fatal("pinned opener was not consulted")
	}
	s := newTestSchema(t, &Core{}, map[string]any{"filesink": cfg})
	c := &fail.Collector{}
	s.Apply(c, files, src)
	if c.Len() != 0 || cfg.Path != "pinned.log" {
		t.Errorf("pinned file not applied: %v, %+v", c.All(), cfg)
	}
}

func TestPinnedSymlinkRejectionIsLoud(t *testing.T) {
	src := Sources{
		Locations: []Location{{Base: "/opt/box/cat-config", Pinned: true}},
		Stat:      fakeStat(map[string]string{"/opt/box/cat-config.json": `{}`}),
		OpenPinned: func(path string) (io.ReadCloser, error) {
			return nil, errors.New("is a symlink: refusing")
		},
	}
	c := &fail.Collector{}
	LoadFiles(c, src, "")
	if c.Len() == 0 {
		t.Error("a symlinked companion must be a startup error, not a skip")
	}
}

func TestPinnedLocationWithoutOpenerFails(t *testing.T) {
	disk := map[string]string{"/opt/box/cat-config.json": `{}`}
	src := Sources{
		Locations: []Location{{Base: "/opt/box/cat-config", Pinned: true}},
		Stat:      fakeStat(disk),
		Open:      fakeFS(disk),
	}
	c := &fail.Collector{}
	LoadFiles(c, src, "")
	if c.Len() == 0 {
		t.Error("a pinned location with an existing candidate but no pinned opener must fail")
	}
}

func TestOversizedConfigIsRefusedWithoutOpening(t *testing.T) {
	big := `{"filesink": {"path": "` + strings.Repeat("x", 100) + `"}}`
	disk := map[string]string{"/etc/cat/config.json": big}
	opened := false
	src := Sources{
		Locations: []Location{{Base: "/etc/cat/config"}},
		Stat:      fakeStat(disk),
		Open: func(path string) (io.ReadCloser, error) {
			opened = true
			return fakeFS(disk)(path)
		},
		MaxSize: 64,
	}
	c := &fail.Collector{}
	LoadFiles(c, src, "")
	if c.Len() == 0 {
		t.Error("an oversized config file must be refused")
	}
	if opened {
		t.Error("an oversized config must never be opened, let alone read")
	}
	src.MaxSize = int64(len(big)) + 1
	c = &fail.Collector{}
	LoadFiles(c, src, "")
	if c.Len() != 0 {
		t.Errorf("a file within the cap must load: %v", c.All())
	}
	if !opened {
		t.Error("a file within the cap must be opened")
	}
}

func TestMissingStatIsAViolation(t *testing.T) {
	c := &fail.Collector{}
	LoadFiles(c, Sources{Locations: []Location{{Base: "/etc/cat/config"}}}, "")
	if c.Len() == 0 {
		t.Error("a missing stat function must be a violation")
	}
}

func TestSuppressedFileKeyIsLoud(t *testing.T) {
	disk := map[string]string{"/etc/cat/config.json": `{"core": {"override": ["a=b"]}}`}
	src := Sources{
		Locations:    []Location{{Base: "/etc/cat/config"}},
		Stat:         fakeStat(disk),
		Open:         fakeFS(disk),
		SuppressCore: []string{"override"},
	}
	files := mustLoadFiles(t, src, "")
	c := &fail.Collector{}
	files.ApplyCore(c, "cat", src)
	if c.Len() == 0 {
		t.Error("a suppressed key in the core file section must be a loud error")
	}
}

func TestUnknownKeyInOwnedSection(t *testing.T) {
	cfg := &loadSink{}
	disk := map[string]string{"/etc/cat/config.json": `{"filesink": {"pth": "typo.log"}}`}
	src := Sources{
		Locations: []Location{{Base: "/etc/cat/config"}},
		Stat:      fakeStat(disk),
		Open:      fakeFS(disk),
	}
	files := mustLoadFiles(t, src, "")
	s := newTestSchema(t, &Core{}, map[string]any{"filesink": cfg})
	c := &fail.Collector{}
	s.Apply(c, files, src)
	if c.Len() == 0 {
		t.Error("unknown key in an owned section must be an error")
	}
}

func TestApplyCoreSeesFileValues(t *testing.T) {
	disk := map[string]string{"/etc/cat/config.json": `{"core": {"disable": ["never"], "override": ["sqlite=mysql"]}}`}
	src := Sources{
		Args:      []string{"--enable", "mysql"},
		LookupEnv: env(map[string]string{"CAT_DISABLE": "sqlite,boltdb"}),
		Locations: []Location{{Base: "/etc/cat/config"}},
		Stat:      fakeStat(disk),
		Open:      fakeFS(disk),
	}
	files := mustLoadFiles(t, src, "")
	c := &fail.Collector{}
	core := files.ApplyCore(c, "cat", src)
	if c.Len() != 0 {
		t.Fatalf("unexpected core errors: %v", c.All())
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
	c := &fail.Collector{}
	core := PeekCore(c, "cat", Sources{
		Args:      []string{"--unknown-service-flag", "junk", "--config=x.yml", "--write-config"},
		LookupEnv: env(map[string]string{"CAT_HELP": "true"}),
	})
	if c.Len() != 0 {
		t.Fatalf("unexpected errors: %v", c.All())
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
	for _, want := range []string{`"maxAge": "1h30m0s"`, `"path": "a.log"`, `"size": 5`, `"rotation"`} {
		if !strings.Contains(text, want) {
			t.Errorf("dump missing %s:\n%s", want, text)
		}
	}
	for _, transient := range []string{`"config"`, `"writeConfig"`, `"help"`} {
		if strings.Contains(text, transient) {
			t.Errorf("run-scoped core field %s must not be dumped (a written config would be self-triggering):\n%s", transient, text)
		}
	}
}
