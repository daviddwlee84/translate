package tts

import (
	"context"
	"fmt"
	"strings"
)

// player plays an audio file via a discovered command-line player. Discovery and
// argv are per-OS; the file path is always passed as a discrete argument.
type player struct {
	goos   string
	runner runner
	forced string // config override binary; "" => auto-discover
}

// playerSpec is one candidate player: its binary and how to build its argv for a
// given file. stdin, when non-empty, is fed on the child's standard input.
type playerSpec struct {
	bin   string
	args  func(file string) []string
	stdin func(file string) string
}

// argsOnly builds a spec that passes the file as its single positional arg.
func argsOnly(bin string) playerSpec {
	return playerSpec{bin: bin, args: func(f string) []string { return []string{f} }}
}

// playerSpecs returns the ordered player candidates for goos.
func playerSpecs(goos string) []playerSpec {
	ffplay := playerSpec{bin: "ffplay", args: func(f string) []string {
		return []string{"-nodisp", "-autoexit", "-loglevel", "quiet", f}
	}}
	switch goos {
	case "darwin":
		return []playerSpec{argsOnly("afplay"), ffplay}
	case "linux":
		return []playerSpec{
			{bin: "mpv", args: func(f string) []string { return []string{"--no-video", "--really-quiet", f} }},
			ffplay,
			{bin: "mpg123", args: func(f string) []string { return []string{"-q", f} }},
			argsOnly("paplay"),
			argsOnly("aplay"),
		}
	case "windows":
		// PowerShell MediaPlayer, with the (program-controlled) path read from
		// stdin so it is never interpolated into the command string.
		return []playerSpec{{
			bin:   "powershell",
			args:  func(string) []string { return []string{"-NoProfile", "-NonInteractive", "-Command", winPlayScript} },
			stdin: func(f string) string { return f },
		}}
	}
	return nil
}

// winPlayScript plays the MP3 whose path is read from stdin, blocking until the
// clip finishes.
const winPlayScript = `Add-Type -AssemblyName presentationCore;` +
	`$p=[Console]::In.ReadToEnd().Trim();` +
	`$m=New-Object System.Windows.Media.MediaPlayer;` +
	`$m.Open([uri]$p);$m.Play();Start-Sleep -Milliseconds 300;` +
	`while(-not $m.NaturalDuration.HasTimeSpan){Start-Sleep -Milliseconds 100};` +
	`while($m.Position -lt $m.NaturalDuration.TimeSpan){Start-Sleep -Milliseconds 200}`

// pick returns the first available player (the forced binary, if set, wins).
func (p player) pick() (playerSpec, bool) {
	specs := playerSpecs(p.goos)
	if p.forced != "" {
		specs = append([]playerSpec{argsOnly(p.forced)}, specs...)
	}
	for _, s := range specs {
		if _, err := p.runner.look(s.bin); err == nil {
			return s, true
		}
	}
	return playerSpec{}, false
}

// available reports whether any audio player is present.
func (p player) available() bool {
	_, ok := p.pick()
	return ok
}

// play plays file with the first available player.
func (p player) play(ctx context.Context, file string) error {
	spec, ok := p.pick()
	if !ok {
		return ErrNoPlayer
	}
	// Defense in depth: the path is program-controlled, but refuse anything odd
	// before handing it to a player.
	if strings.ContainsAny(file, "\n\r") {
		return fmt.Errorf("tts: invalid audio path")
	}
	stdin := ""
	if spec.stdin != nil {
		stdin = spec.stdin(file)
	}
	return p.runner.run(ctx, spec.bin, stdin, spec.args(file)...)
}
