package tts

import (
	"context"
	"strconv"
	"strings"

	"github.com/daviddwlee84/translate/internal/lang"
)

// longTextThreshold is the rune count above which macOS `say` is fed via stdin
// (`say -f -`) instead of an argv positional, to dodge ARG_MAX limits.
const longTextThreshold = 1000

// winSpeakScript speaks text read from stdin via the Windows SAPI. Feeding the
// text on stdin (never interpolated into the command) avoids any quoting/injection.
const winSpeakScript = `Add-Type -AssemblyName System.Speech;` +
	`$s=New-Object System.Speech.Synthesis.SpeechSynthesizer;` +
	`$s.Speak([Console]::In.ReadToEnd())`

// nativeBackend speaks via the OS's built-in offline TTS: macOS `say`, Linux
// espeak-ng/espeak/spd-say, or Windows PowerShell SAPI. goos is a parameter (not
// a build tag) so all three argv shapes compile and unit-test from one binary.
type nativeBackend struct {
	goos   string
	runner runner
	voices map[string]string // user voice overrides (lang -> voice)
	rate   int               // words/min; 0 => backend default
}

func newNative(goos string, r runner, voices map[string]string, rate int) *nativeBackend {
	return &nativeBackend{goos: goos, runner: r, voices: voices, rate: rate}
}

func (n *nativeBackend) Name() string { return "native" }

// primaryBinary is the offline TTS binary for this OS, or "" if none is known.
// On Linux it returns the first of espeak-ng/espeak/spd-say found in PATH.
func (n *nativeBackend) primaryBinary() string {
	switch n.goos {
	case "darwin":
		return "say"
	case "linux":
		for _, b := range []string{"espeak-ng", "espeak", "spd-say"} {
			if _, err := n.runner.look(b); err == nil {
				return b
			}
		}
		return ""
	case "windows":
		return "powershell"
	}
	return ""
}

// Available reports whether this OS's native TTS binary is present.
func (n *nativeBackend) Available() bool {
	bin := n.primaryBinary()
	if bin == "" {
		return false
	}
	_, err := n.runner.look(bin)
	return err == nil
}

// Speak synthesizes text in langCode using the native binary, blocking until
// playback finishes (or ctx is cancelled).
func (n *nativeBackend) Speak(ctx context.Context, text, langCode string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return ErrEmptyText
	}
	switch n.goos {
	case "darwin":
		return n.speakSay(ctx, text, langCode)
	case "linux":
		return n.speakLinux(ctx, text, langCode)
	case "windows":
		return n.runner.run(ctx, "powershell", text,
			"-NoProfile", "-NonInteractive", "-Command", winSpeakScript)
	}
	return ErrNoBackend
}

func (n *nativeBackend) speakSay(ctx context.Context, text, langCode string) error {
	voice := sayVoice(n.voices, langCode)
	long := len([]rune(text)) > longTextThreshold
	build := func(withVoice bool) (args []string, stdin string) {
		if withVoice && voice != "" {
			args = append(args, "-v", voice)
		}
		if n.rate > 0 {
			args = append(args, "-r", strconv.Itoa(n.rate))
		}
		if long {
			args = append(args, "-f", "-") // read text from stdin
			stdin = text
		} else {
			args = append(args, text) // discrete arg — never a shell string
		}
		return args, stdin
	}
	args, stdin := build(true)
	err := n.runner.run(ctx, "say", stdin, args...)
	// A missing/renamed voice fails `say`; retry once with the default voice
	// (unless the user cancelled).
	if err != nil && voice != "" && ctx.Err() == nil {
		args, stdin = build(false)
		err = n.runner.run(ctx, "say", stdin, args...)
	}
	return err
}

func (n *nativeBackend) speakLinux(ctx context.Context, text, langCode string) error {
	bin := n.primaryBinary()
	switch bin {
	case "espeak-ng", "espeak":
		var args []string
		if v := espeakVoice(n.voices, langCode); v != "" {
			args = append(args, "-v", v)
		}
		if n.rate > 0 {
			args = append(args, "-s", strconv.Itoa(n.rate))
		}
		args = append(args, text)
		return n.runner.run(ctx, bin, "", args...)
	case "spd-say":
		var args []string
		if b := lang.Base(normKey(langCode)); b != "" {
			args = append(args, "-l", b)
		}
		// -w waits for playback to complete; `--` guards text that starts with '-'.
		args = append(args, "-w", "--", text)
		return n.runner.run(ctx, "spd-say", "", args...)
	}
	return ErrNoBackend
}
