//go:build unix

package syslog

import (
	"context"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// listenUnixgram creates a local datagram syslog endpoint the sink can
// dial — real end-to-end without root or a syslogd.
func listenUnixgram(t *testing.T) (string, *net.UnixConn) {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "log.sock")
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: sock, Net: "unixgram"})
	if err != nil {
		t.Fatalf("cannot listen: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return sock, conn
}

func recv(t *testing.T, conn *net.UnixConn) string {
	t.Helper()
	buf := make([]byte, 4096)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("no datagram arrived: %v", err)
	}
	return string(buf[:n])
}

func configured(t *testing.T, cfg Config) *Syslog {
	t.Helper()
	s := &Syslog{cfg: cfg}
	if err := s.Configured(); err != nil {
		t.Fatalf("Configured failed: %v", err)
	}
	t.Cleanup(func() { s.Stop() })
	return s
}

func local(sock string) Config {
	return Config{Level: "debug", Format: "text", Facility: "daemon", Tag: "boxtest", Network: "unixgram", Address: sock}
}

func TestEndToEndDelivery(t *testing.T) {
	sock, conn := listenUnixgram(t)
	s := configured(t, local(sock))
	slog.New(s).Info("hello", "key", "value")
	msg := recv(t, conn)
	if !strings.HasPrefix(msg, "<30>") { // daemon(24) + info(6)
		t.Errorf("priority wrong: %q", msg)
	}
	if !strings.Contains(msg, "boxtest") || !strings.Contains(msg, "msg=hello") || !strings.Contains(msg, "key=value") {
		t.Errorf("payload wrong: %q", msg)
	}
	if strings.Contains(msg, "level=") {
		t.Errorf("level attribute must be stripped (severity travels in the priority): %q", msg)
	}
}

func TestSeverityMapping(t *testing.T) {
	sock, conn := listenUnixgram(t)
	s := configured(t, local(sock))
	logger := slog.New(s)
	cases := []struct {
		log  func()
		want string
	}{
		{func() { logger.Error("e") }, "<27>"}, // daemon + err(3)
		{func() { logger.Warn("w") }, "<28>"},  // daemon + warning(4)
		{func() { logger.Info("i") }, "<30>"},  // daemon + info(6)
		{func() { logger.Debug("d") }, "<31>"}, // daemon + debug(7)
	}
	for _, tc := range cases {
		tc.log()
		if msg := recv(t, conn); !strings.HasPrefix(msg, tc.want) {
			t.Errorf("want prefix %s, got %q", tc.want, msg)
		}
	}
}

func TestDerivedViewCarriesAttrs(t *testing.T) {
	sock, conn := listenUnixgram(t)
	s := configured(t, local(sock))
	slog.New(s).With("request", "42").WithGroup("db").Info("done", "rows", 3)
	msg := recv(t, conn)
	if !strings.Contains(msg, "request=42") || !strings.Contains(msg, "db.rows=3") {
		t.Errorf("derived view chain wrong: %q", msg)
	}
}

func TestJSONFormat(t *testing.T) {
	sock, conn := listenUnixgram(t)
	cfg := local(sock)
	cfg.Format = "json"
	s := configured(t, cfg)
	slog.New(s).Info("hello")
	if msg := recv(t, conn); !strings.Contains(msg, `"msg":"hello"`) {
		t.Errorf("json payload wrong: %q", msg)
	}
}

func TestLevelGating(t *testing.T) {
	sock, _ := listenUnixgram(t)
	cfg := local(sock)
	cfg.Level = "warn"
	s := configured(t, cfg)
	ctx := context.Background()
	if s.Enabled(ctx, slog.LevelInfo) || !s.Enabled(ctx, slog.LevelError) {
		t.Error("level gate wrong")
	}
}

func TestConfiguredRejections(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{"bad level", Config{Level: "loud", Format: "text", Facility: "daemon"}},
		{"bad format", Config{Level: "info", Format: "xml", Facility: "daemon"}},
		{"bad facility", Config{Level: "info", Format: "text", Facility: "kitchen"}},
		{"network without address", Config{Level: "info", Format: "text", Facility: "daemon", Network: "udp"}},
		{"undialable", Config{Level: "info", Format: "text", Facility: "daemon", Network: "unixgram", Address: "/nonexistent/sock"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Syslog{cfg: tc.cfg}
			if err := s.Configured(); err == nil {
				s.Stop()
				t.Error("expected a Configured error")
			}
		})
	}
}

func TestInertBeforeConfiguredAndAfterStop(t *testing.T) {
	s := &Syslog{}
	ctx := context.Background()
	if s.Enabled(ctx, slog.LevelError) {
		t.Error("unconfigured sink must accept nothing")
	}
	if s.WithAttrs([]slog.Attr{slog.String("a", "b")}) != slog.Handler(s) {
		t.Error("unconfigured WithAttrs must return the receiver")
	}
	sock, _ := listenUnixgram(t)
	s = configured(t, local(sock))
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
