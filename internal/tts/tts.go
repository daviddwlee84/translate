// Package tts provides free, cross-platform text-to-speech for translate. It is
// UI-agnostic (importable by both cmd/* and internal/tui) and shells out only
// through the injectable runner, so tests never spawn a real process.
//
// A Speaker tries an ordered list of backends: a native offline backend (macOS
// `say`, Linux espeak-ng/spd-say, Windows SAPI), then the online Google
// translate_tts fallback (MP3 + a discovered audio player). Each OS thus gets its
// own fallback chain from one binary, with no build tags.
package tts

import (
	"context"
	"errors"
	"runtime"
	"time"
)

// Speaker turns text into speech. Speak blocks until playback finishes or ctx is
// cancelled (which kills the underlying process).
type Speaker interface {
	Name() string
	Available() bool
	Speak(ctx context.Context, text, lang string) error
}

// Options configures New. Zero values select sensible defaults.
type Options struct {
	Order     []string          // backend order; default ["native","google"]
	Rate      int               // native words/min; 0 => backend default
	Voices    map[string]string // lang -> native voice override
	GoogleURL string            // translate_tts endpoint override
	UserAgent string
	Timeout   time.Duration
	CacheDir  string // "" => <XDG cache>/translate/tts
	Player    string // forced player binary; "" => auto-discover

	goos   string // test seam; "" => runtime.GOOS
	runner runner // test seam; nil => execRunner
}

// New builds a Speaker that tries each configured backend in order.
func New(opt Options) Speaker {
	goos := opt.goos
	if goos == "" {
		goos = runtime.GOOS
	}
	var r runner = opt.runner
	if r == nil {
		r = execRunner{}
	}
	order := opt.Order
	if len(order) == 0 {
		order = []string{"native", "google"}
	}
	pl := player{goos: goos, runner: r, forced: opt.Player}
	build := map[string]Speaker{
		"native": newNative(goos, r, opt.Voices, opt.Rate),
		"google": newGoogle(opt.GoogleURL, opt.UserAgent, opt.CacheDir, opt.Timeout, pl),
	}
	var backends []Speaker
	for _, name := range order {
		if b, ok := build[name]; ok {
			backends = append(backends, b)
		}
	}
	return &fallback{backends: backends}
}

// fallback tries each backend in order, skipping unavailable ones and advancing
// to the next on error. It never panics on an empty backend list.
type fallback struct{ backends []Speaker }

func (f *fallback) Name() string { return "fallback" }

func (f *fallback) Available() bool {
	for _, b := range f.backends {
		if b.Available() {
			return true
		}
	}
	return false
}

func (f *fallback) Speak(ctx context.Context, text, lang string) error {
	var errs []error
	tried := false
	for _, b := range f.backends {
		if !b.Available() {
			continue
		}
		tried = true
		err := b.Speak(ctx, text, lang)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err() // user cancelled — don't fall through to another backend
		}
		errs = append(errs, err)
	}
	if !tried {
		return ErrNoBackend
	}
	return errors.Join(errs...)
}
