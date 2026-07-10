// Black-box integration tests: this file lives in the external test
// package on purpose — only the public API is reachable, so the tests
// cannot slip into internals. Each test re-execs the test binary as a
// framework binary (TestMain switches on a personality env var and
// calls Main), driving real dispatch, real config files, real env, real
// exit codes.
package sxclifw_test

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	sxclifw "github.com/sxcli/sxcli-fw"
	_ "github.com/sxcli/sxcli-fw/configfmt/yaml"
	_ "github.com/sxcli/sxcli-fw/sink/console"
	_ "github.com/sxcli/sxcli-fw/sink/file"
)

const personalityVar = "SXCLI_TEST_PERSONALITY"

func TestMain(m *testing.M) {
	personality := os.Getenv(personalityVar)
	if personality == "" {
		os.Exit(m.Run())
	}
	if personality == "single" || personality == "multi" || personality == "hardened" {
		registerProbe()
		registerGreeter()
	}
	if personality == "multi" {
		registerEcho()
	}
	if personality == "hardened" {
		sxclifw.Suppress(sxclifw.FeatureConfigFile)
	}
	sxclifw.Main()
}

// ---- the personalities' services ----------------------------------------

type greeter interface{ Greet() string }

type greeterService struct{}

func (g *greeterService) Greet() string     { return "hi" }
func (g *greeterService) Configured() error { return nil }

type probeConfig struct {
	Exit int    `json:"exit" arg:"exit"`
	Note string `json:"note" arg:"note,n" usage:"a note to print"`
}

type probeApplet struct {
	cfg probeConfig
	G   greeter `inject:";optional"`
}

func (p *probeApplet) Configured() error { return nil }

func (p *probeApplet) Run() int {
	greeted := p.G != nil && p.G.Greet() == "hi"
	fmt.Printf("note=%s greeted=%v positionals=%v\n", p.cfg.Note, greeted, sxclifw.Positionals())
	slog.Info("probe ran", "note", p.cfg.Note)
	return p.cfg.Exit
}

type echoApplet struct{}

func (e *echoApplet) Configured() error { return nil }
func (e *echoApplet) Run() int {
	fmt.Println("echo applet")
	return 0
}

func registerProbe() {
	p := &probeApplet{}
	sxclifw.Register("probe", p, sxclifw.WithConfig(&p.cfg))
}

func registerGreeter() {
	sxclifw.Register("greeter", &greeterService{}, sxclifw.Provides[greeter]())
	sxclifw.Register("loud", &loudService{})
}

// loudService is referenced by nobody: genuinely cold unless enabled.
type loudService struct{}

func (l *loudService) Configured() error { return nil }
func (l *loudService) Start() error {
	slog.Info("loud service started")
	return nil
}
func (l *loudService) Stop() error { return nil }

func registerEcho() {
	sxclifw.Register("echo", &echoApplet{}, sxclifw.WithConfig(&struct {
		Quiet bool `json:"quiet" arg:"quiet,q"`
	}{}))
}

// ---- harness -------------------------------------------------------------

type result struct {
	code   int
	stdout string
	stderr string
}

func box(t *testing.T, personality string, env map[string]string, binary string, args ...string) result {
	t.Helper()
	if binary == "" {
		binary = os.Args[0]
	}
	cmd := exec.Command(binary, args...)
	cmd.Env = append(os.Environ(),
		personalityVar+"="+personality,
		"XDG_CONFIG_HOME="+t.TempDir(), // keep the user config location empty and hermetic
	)
	for name, value := range env {
		cmd.Env = append(cmd.Env, name+"="+value)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		t.Fatalf("cannot run the box: %v", err)
	}
	return result{code: cmd.ProcessState.ExitCode(), stdout: stdout.String(), stderr: stderr.String()}
}

// ---- tests ---------------------------------------------------------------

func TestSingleAppletRunsWithArgsAndPositionals(t *testing.T) {
	r := box(t, "single", nil, "", "--note", "hello", "one", "two")
	if r.code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, "note=hello") || !strings.Contains(r.stdout, "positionals=[one two]") {
		t.Errorf("stdout wrong:\n%s", r.stdout)
	}
}

func TestExitCodePropagates(t *testing.T) {
	if r := box(t, "single", nil, "", "--exit", "7"); r.code != 7 {
		t.Errorf("exit = %d, want 7", r.code)
	}
}

func TestMultiAppletSelectorDispatch(t *testing.T) {
	r := box(t, "multi", nil, "", "echo")
	if r.code != 0 || !strings.Contains(r.stdout, "echo applet") {
		t.Errorf("selector dispatch failed: exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
}

func TestSymlinkDispatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on windows")
	}
	link := filepath.Join(t.TempDir(), "echo")
	if err := os.Symlink(os.Args[0], link); err != nil {
		t.Fatal(err)
	}
	r := box(t, "multi", nil, link)
	if r.code != 0 || !strings.Contains(r.stdout, "echo applet") {
		t.Errorf("argv[0] dispatch failed: exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
}

func TestUnknownAppletPrintsUsage(t *testing.T) {
	r := box(t, "multi", nil, "", "ghost")
	if r.code == 0 {
		t.Fatal("unknown applet must fail")
	}
	if !strings.Contains(r.stderr, "usage:") || !strings.Contains(r.stderr, "probe") || !strings.Contains(r.stderr, "echo") {
		t.Errorf("usage dump wrong:\n%s", r.stderr)
	}
}

func TestUnknownArgumentFailsLoudly(t *testing.T) {
	r := box(t, "single", nil, "", "--nope")
	if r.code == 0 || !strings.Contains(r.stderr, "unknown argument --nope") {
		t.Errorf("exit %d, stderr:\n%s", r.code, r.stderr)
	}
}

func TestConfigFilePrecedence(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "probe.json")
	if err := os.WriteFile(cfg, []byte(`{"probe": {"note": "fromfile"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r := box(t, "single", nil, "", "--config", cfg)
	if !strings.Contains(r.stdout, "note=fromfile") {
		t.Errorf("file value not applied:\n%s%s", r.stdout, r.stderr)
	}
	r = box(t, "single", map[string]string{"PROBE_NOTE": "fromenv"}, "", "--config", cfg)
	if !strings.Contains(r.stdout, "note=fromenv") {
		t.Errorf("env must beat file:\n%s%s", r.stdout, r.stderr)
	}
	r = box(t, "single", map[string]string{"PROBE_NOTE": "fromenv"}, "", "--config", cfg, "--note", "fromarg")
	if !strings.Contains(r.stdout, "note=fromarg") {
		t.Errorf("arg must beat env:\n%s%s", r.stdout, r.stderr)
	}
}

func TestYAMLConfigFile(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "probe.yaml")
	if err := os.WriteFile(cfg, []byte("probe:\n  note: fromyaml\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := box(t, "single", nil, "", "--config", cfg)
	if !strings.Contains(r.stdout, "note=fromyaml") {
		t.Errorf("yaml config not applied:\n%s%s", r.stdout, r.stderr)
	}
}

func TestWriteConfigRoundTrip(t *testing.T) {
	r := box(t, "single", nil, "", "--write-config", "--note", "dumped")
	if r.code != 0 || !strings.Contains(r.stdout, `"note": "dumped"`) {
		t.Fatalf("stdout dump wrong: exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	target := filepath.Join(t.TempDir(), "out.yaml")
	if r = box(t, "single", nil, "", "--write-config", "--config", target, "--note", "roundtrip"); r.code != 0 {
		t.Fatalf("write to yaml failed: exit %d\n%s", r.code, r.stderr)
	}
	if r = box(t, "single", nil, "", "--config", target); !strings.Contains(r.stdout, "note=roundtrip") {
		t.Errorf("written yaml must load back:\n%s%s", r.stdout, r.stderr)
	}
}

func TestHelpListsClosureArguments(t *testing.T) {
	r := box(t, "single", nil, "", "--help")
	if r.code != 0 {
		t.Fatalf("help exit %d\n%s", r.code, r.stderr)
	}
	for _, want := range []string{"--note, -n", "a note to print", "--config, -c", "--console-level"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("help misses %q:\n%s", want, r.stdout)
		}
	}
}

func TestConsoleSinkWritesStderr(t *testing.T) {
	r := box(t, "single", nil, "", "--note", "logged")
	if !strings.Contains(r.stderr, "probe ran") || !strings.Contains(r.stderr, "note=logged") {
		t.Errorf("console sink output missing:\n%s", r.stderr)
	}
}

func TestOptionalDependencyActivatesItsMatch(t *testing.T) {
	// per the resolution rules a bare single field pulls its first
	// registered match into the closure — optional only tolerates zero
	// matches
	r := box(t, "single", nil, "")
	if !strings.Contains(r.stdout, "greeted=true") {
		t.Errorf("registered match of an optional field must be active:\n%s%s", r.stdout, r.stderr)
	}
}

func TestDisableSteersInjection(t *testing.T) {
	r := box(t, "single", nil, "", "--disable", "greeter")
	if !strings.Contains(r.stdout, "greeted=false") {
		t.Errorf("disabled service must leave the optional field nil:\n%s%s", r.stdout, r.stderr)
	}
}

func TestEnableActivatesUnreferencedService(t *testing.T) {
	r := box(t, "single", nil, "")
	if strings.Contains(r.stderr, "loud service started") {
		t.Fatalf("unreferenced service must be cold by default:\n%s", r.stderr)
	}
	r = box(t, "single", nil, "", "--enable", "loud")
	if !strings.Contains(r.stderr, "loud service started") {
		t.Errorf("enabled service must start:\n%s", r.stderr)
	}
}

func TestLogfileSinkEndToEnd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "box.log")
	r := box(t, "single", nil, "", "--enable", "logfile", "--logfile-path", path, "--note", "tofile")
	if r.code != 0 {
		t.Fatalf("exit %d\n%s", r.code, r.stderr)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("log file missing: %v", err)
	}
	if !strings.Contains(string(content), "probe ran") || !strings.Contains(string(content), "note=tofile") {
		t.Errorf("log file content wrong:\n%s", content)
	}
}

func TestSuppressedConfigFlagIsRefused(t *testing.T) {
	r := box(t, "hardened", nil, "", "--config", "/tmp/x.json")
	if r.code == 0 || !strings.Contains(r.stderr, "unknown argument --config") {
		t.Errorf("suppressed --config must be unknown: exit %d\n%s", r.code, r.stderr)
	}
}
