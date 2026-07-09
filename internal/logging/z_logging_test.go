package logging

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// recorder is a fake sink: it records message strings, gates on a
// minimum level and can inject a Handle error. Its views share the
// parent's note store, mimicking real sinks sharing their resource.
type recorder struct {
	mu    sync.Mutex
	min   slog.Level
	notes []string
	fail  error
}

func (r *recorder) note(s string) {
	r.mu.Lock()
	r.notes = append(r.notes, s)
	r.mu.Unlock()
}

func (r *recorder) taken() []string {
	r.mu.Lock()
	out := append([]string{}, r.notes...)
	r.mu.Unlock()
	return out
}

func (r *recorder) Enabled(_ context.Context, level slog.Level) bool {
	return level >= r.min
}

func (r *recorder) Handle(_ context.Context, record slog.Record) error {
	r.note(record.Message)
	return r.fail
}

func (r *recorder) WithAttrs(attrs []slog.Attr) slog.Handler {
	return recorderView{recorder: r, tag: renderAttrs("", attrs)}
}

func (r *recorder) WithGroup(name string) slog.Handler {
	return recorderView{recorder: r, tag: name + ">"}
}

// recorderView is a derived view over a recorder, tagging messages with
// the attr/group chain it carries.
type recorderView struct {
	*recorder
	tag string
}

func (v recorderView) Handle(_ context.Context, record slog.Record) error {
	v.note(v.tag + record.Message)
	return v.fail
}

func (v recorderView) WithAttrs(attrs []slog.Attr) slog.Handler {
	return recorderView{recorder: v.recorder, tag: renderAttrs(v.tag, attrs)}
}

func (v recorderView) WithGroup(name string) slog.Handler {
	return recorderView{recorder: v.recorder, tag: v.tag + name + ">"}
}

func renderAttrs(prefix string, attrs []slog.Attr) string {
	for _, a := range attrs {
		prefix += a.Key + "=" + a.Value.String() + ";"
	}
	return prefix
}

func TestMultiFanOutAndLevelGating(t *testing.T) {
	all := &recorder{min: slog.LevelDebug}
	warnOnly := &recorder{min: slog.LevelWarn}
	m := NewMulti(all, warnOnly)
	ctx := context.Background()
	if !m.Enabled(ctx, slog.LevelDebug) {
		t.Error("Enabled must be true when any child accepts")
	}
	if err := m.Handle(ctx, slog.Record{Level: slog.LevelInfo, Message: "hello"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(all.taken()) != 1 || len(warnOnly.taken()) != 0 {
		t.Errorf("level gating wrong: all=%v warn=%v", all.taken(), warnOnly.taken())
	}
}

func TestMultiEnabledFalseWhenNoChildAccepts(t *testing.T) {
	m := NewMulti(&recorder{min: slog.LevelError})
	if m.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Enabled must be false when no child accepts")
	}
}

func TestMultiJoinsErrorsAndKeepsDelivering(t *testing.T) {
	bad := &recorder{min: slog.LevelDebug, fail: errors.New("sink broke")}
	good := &recorder{min: slog.LevelDebug}
	m := NewMulti(bad, good)
	err := m.Handle(context.Background(), slog.Record{Level: slog.LevelInfo, Message: "x"})
	if err == nil || !strings.Contains(err.Error(), "sink broke") {
		t.Errorf("child error must surface: %v", err)
	}
	if len(good.taken()) != 1 {
		t.Error("a failing sink must not block the others")
	}
}

func TestMultiDerivesChildViews(t *testing.T) {
	sink := &recorder{min: slog.LevelDebug}
	m := NewMulti(sink)
	derived := m.WithGroup("req").WithAttrs([]slog.Attr{slog.String("id", "42")})
	if err := derived.Handle(context.Background(), slog.Record{Level: slog.LevelInfo, Message: "done"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	notes := sink.taken()
	if len(notes) != 1 || notes[0] != "req>id=42;done" {
		t.Errorf("derived view chain wrong: %v", notes)
	}
}

func TestMultiWithAttrsAndGroupIdentity(t *testing.T) {
	m := NewMulti(&recorder{})
	if m.WithAttrs(nil) != slog.Handler(m) {
		t.Error("empty attrs must return the receiver")
	}
	if m.WithGroup("") != slog.Handler(m) {
		t.Error("empty group must return the receiver")
	}
}

func TestBufferCapturesEverythingAndReplaysInOrder(t *testing.T) {
	b := NewBuffer()
	ctx := context.Background()
	if !b.Enabled(ctx, slog.LevelDebug-8) {
		t.Error("buffer must capture every level")
	}
	for i := 0; i < 3; i++ {
		b.Handle(ctx, slog.Record{Level: slog.LevelInfo, Message: fmt.Sprintf("m%d", i)})
	}
	if b.Len() != 3 {
		t.Fatalf("expected 3 captured, got %d", b.Len())
	}
	sink := &recorder{min: slog.LevelDebug}
	if err := b.Replay(sink); err != nil {
		t.Fatalf("replay failed: %v", err)
	}
	if fmt.Sprint(sink.taken()) != fmt.Sprint([]string{"m0", "m1", "m2"}) {
		t.Errorf("replay order wrong: %v", sink.taken())
	}
}

func TestBufferReplayReproducesViewChains(t *testing.T) {
	b := NewBuffer()
	logger := slog.New(b)
	logger.WithGroup("req").With("id", "42").Info("handled")
	logger.Info("plain")

	var out bytes.Buffer
	target := slog.NewTextHandler(&out, &slog.HandlerOptions{})
	if err := b.Replay(target); err != nil {
		t.Fatalf("replay failed: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "req.id=42") || !strings.Contains(text, "msg=handled") {
		t.Errorf("group/attr chain lost in replay:\n%s", text)
	}
	if !strings.Contains(text, "msg=plain") {
		t.Errorf("plain record lost:\n%s", text)
	}
}

func TestBufferReplayRespectsTargetEnabled(t *testing.T) {
	b := NewBuffer()
	ctx := context.Background()
	b.Handle(ctx, slog.Record{Level: slog.LevelDebug, Message: "quiet"})
	b.Handle(ctx, slog.Record{Level: slog.LevelError, Message: "loud"})
	sink := &recorder{min: slog.LevelWarn}
	if err := b.Replay(sink); err != nil {
		t.Fatalf("replay failed: %v", err)
	}
	if notes := sink.taken(); len(notes) != 1 || notes[0] != "loud" {
		t.Errorf("target level gate ignored: %v", notes)
	}
}

func TestBufferConcurrentSmoke(t *testing.T) {
	b := NewBuffer()
	logger := slog.New(b)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				logger.With("worker", fmt.Sprint(n)).Info("tick")
			}
		}(i)
	}
	wg.Wait()
	if b.Len() != 400 {
		t.Errorf("expected 400 captured records, got %d", b.Len())
	}
}

func TestEndToEndBufferIntoMulti(t *testing.T) {
	b := NewBuffer()
	slog.New(b).Warn("early warning")
	console := &recorder{min: slog.LevelInfo}
	file := &recorder{min: slog.LevelDebug}
	if err := b.Replay(NewMulti(console, file)); err != nil {
		t.Fatalf("replay failed: %v", err)
	}
	if len(console.taken()) != 1 || len(file.taken()) != 1 {
		t.Errorf("early record must reach every accepting sink: console=%v file=%v", console.taken(), file.taken())
	}
}
