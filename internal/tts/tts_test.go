package tts

import (
	"context"
	"errors"
	"testing"
)

// fakeRunner records calls and never spawns a real process.
type fakeRunner struct {
	present map[string]bool
	calls   []fakeCall
	err     error // error returned by run (nil => success)
}

type fakeCall struct {
	name  string
	stdin string
	args  []string
}

func (f *fakeRunner) look(name string) (string, error) {
	if f.present[name] {
		return "/usr/bin/" + name, nil
	}
	return "", errors.New("not found")
}

func (f *fakeRunner) run(ctx context.Context, name, stdin string, args ...string) error {
	f.calls = append(f.calls, fakeCall{name: name, stdin: stdin, args: args})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return f.err
}

func (f *fakeRunner) lastArgsContain(s string) bool {
	if len(f.calls) == 0 {
		return false
	}
	for _, a := range f.calls[len(f.calls)-1].args {
		if a == s {
			return true
		}
	}
	return false
}

// fakeSpeaker is a programmable Speaker for exercising the fallback orchestrator
// without touching the network or a real backend.
type fakeSpeaker struct {
	name  string
	avail bool
	err   error
	spoke bool
}

func (s *fakeSpeaker) Name() string    { return s.name }
func (s *fakeSpeaker) Available() bool { return s.avail }
func (s *fakeSpeaker) Speak(ctx context.Context, text, lang string) error {
	s.spoke = true
	return s.err
}

func TestFallbackOrder(t *testing.T) {
	a := &fakeSpeaker{name: "a", avail: true}
	b := &fakeSpeaker{name: "b", avail: true}
	f := &fallback{backends: []Speaker{a, b}}
	if err := f.Speak(context.Background(), "hi", "en"); err != nil {
		t.Fatalf("Speak: %v", err)
	}
	if !a.spoke || b.spoke {
		t.Errorf("expected only first backend to speak; a=%v b=%v", a.spoke, b.spoke)
	}
}

func TestFallbackSkipsUnavailableAndAdvancesOnError(t *testing.T) {
	down := &fakeSpeaker{name: "down", avail: false}
	failing := &fakeSpeaker{name: "fail", avail: true, err: errors.New("boom")}
	ok := &fakeSpeaker{name: "ok", avail: true}
	f := &fallback{backends: []Speaker{down, failing, ok}}
	if err := f.Speak(context.Background(), "hi", "en"); err != nil {
		t.Fatalf("Speak: %v", err)
	}
	if down.spoke {
		t.Error("unavailable backend should not speak")
	}
	if !failing.spoke || !ok.spoke {
		t.Errorf("expected failing then ok to run; failing=%v ok=%v", failing.spoke, ok.spoke)
	}
}

func TestFallbackNoBackend(t *testing.T) {
	f := &fallback{backends: []Speaker{&fakeSpeaker{name: "x", avail: false}}}
	if err := f.Speak(context.Background(), "hi", "en"); !errors.Is(err, ErrNoBackend) {
		t.Errorf("err = %v, want ErrNoBackend", err)
	}
}

func TestFallbackCancelStops(t *testing.T) {
	// A cancelled context returns ctx.Err() and must not fall through to a second
	// backend (the user asked to stop).
	first := &fakeSpeaker{name: "first", avail: true, err: context.Canceled}
	second := &fakeSpeaker{name: "second", avail: true}
	f := &fallback{backends: []Speaker{first, second}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.Speak(ctx, "hi", "en"); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if second.spoke {
		t.Error("must not advance to the next backend after cancellation")
	}
}

func TestNativeSayArgv(t *testing.T) {
	r := &fakeRunner{present: map[string]bool{"say": true}}
	n := newNative("darwin", r, nil, 0)
	if !n.Available() {
		t.Fatal("say should be available")
	}
	if err := n.Speak(context.Background(), "測試", "zh-TW"); err != nil {
		t.Fatalf("Speak: %v", err)
	}
	if len(r.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(r.calls))
	}
	c := r.calls[0]
	if c.name != "say" {
		t.Errorf("bin = %q, want say", c.name)
	}
	if !r.lastArgsContain("Meijia") || !r.lastArgsContain("-v") {
		t.Errorf("expected -v Meijia in args, got %v", c.args)
	}
	// Text must be a discrete arg (never a shell string), and NOT on stdin.
	if last := c.args[len(c.args)-1]; last != "測試" {
		t.Errorf("last arg = %q, want the text 測試", last)
	}
	if c.stdin != "" {
		t.Errorf("short text should not use stdin, got %q", c.stdin)
	}
}

func TestNativeSayVoiceRetry(t *testing.T) {
	// A failing `say` (e.g. missing voice) retries once without -v.
	r := &fakeRunner{present: map[string]bool{"say": true}, err: errors.New("voice not found")}
	n := newNative("darwin", r, nil, 0)
	_ = n.Speak(context.Background(), "hi", "en")
	if len(r.calls) != 2 {
		t.Fatalf("calls = %d, want 2 (voice then default)", len(r.calls))
	}
	for _, a := range r.calls[1].args {
		if a == "-v" {
			t.Error("retry call should not pass -v")
		}
	}
}

func TestNativeWindowsUsesStdin(t *testing.T) {
	r := &fakeRunner{present: map[string]bool{"powershell": true}}
	n := newNative("windows", r, nil, 0)
	if err := n.Speak(context.Background(), "hello", "en"); err != nil {
		t.Fatalf("Speak: %v", err)
	}
	c := r.calls[0]
	if c.name != "powershell" {
		t.Errorf("bin = %q, want powershell", c.name)
	}
	if c.stdin != "hello" {
		t.Errorf("windows text must be on stdin, got stdin=%q", c.stdin)
	}
	if !r.lastArgsContain("-Command") {
		t.Errorf("expected -Command, got %v", c.args)
	}
}

func TestNativeLinuxEspeak(t *testing.T) {
	r := &fakeRunner{present: map[string]bool{"espeak-ng": true}}
	n := newNative("linux", r, nil, 0)
	if err := n.Speak(context.Background(), "hola", "es"); err != nil {
		t.Fatalf("Speak: %v", err)
	}
	c := r.calls[0]
	if c.name != "espeak-ng" {
		t.Errorf("bin = %q, want espeak-ng", c.name)
	}
	if last := c.args[len(c.args)-1]; last != "hola" {
		t.Errorf("last arg = %q, want hola", last)
	}
}

func TestNativeCtxCancel(t *testing.T) {
	r := &fakeRunner{present: map[string]bool{"say": true}}
	n := newNative("darwin", r, nil, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := n.Speak(ctx, "hi", "en")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if len(r.calls) != 1 {
		t.Errorf("cancelled say should not retry; calls = %d", len(r.calls))
	}
}

func TestPlayerPickAndPlay(t *testing.T) {
	// afplay absent → falls to ffplay with the no-display flags.
	r := &fakeRunner{present: map[string]bool{"ffplay": true}}
	p := player{goos: "darwin", runner: r}
	if !p.available() {
		t.Fatal("ffplay should make a player available")
	}
	if err := p.play(context.Background(), "/tmp/a.mp3"); err != nil {
		t.Fatalf("play: %v", err)
	}
	c := r.calls[0]
	if c.name != "ffplay" {
		t.Errorf("player = %q, want ffplay", c.name)
	}
	if !r.lastArgsContain("-nodisp") || !r.lastArgsContain("-autoexit") {
		t.Errorf("ffplay must run headless, got %v", c.args)
	}
}

func TestPlayerNone(t *testing.T) {
	p := player{goos: "linux", runner: &fakeRunner{present: map[string]bool{}}}
	if p.available() {
		t.Error("no players present, should be unavailable")
	}
	if err := p.play(context.Background(), "/tmp/a.mp3"); !errors.Is(err, ErrNoPlayer) {
		t.Errorf("err = %v, want ErrNoPlayer", err)
	}
}

// TestNewOrder verifies New honors the configured backend order (native before
// google) using the injected goos + runner, without any real process/network.
func TestNewOrder(t *testing.T) {
	r := &fakeRunner{present: map[string]bool{"say": true, "afplay": true}}
	sp := New(Options{goos: "darwin", runner: r, Order: []string{"native", "google"}})
	if err := sp.Speak(context.Background(), "hi", "en"); err != nil {
		t.Fatalf("Speak: %v", err)
	}
	if len(r.calls) == 0 || r.calls[0].name != "say" {
		t.Errorf("expected native `say` first, calls = %+v", r.calls)
	}
}
