package tts

import (
	"context"
	"io"
	"os/exec"
	"strings"
)

// runner abstracts binary discovery and process execution so tests can inject a
// fake and never spawn a real process. execRunner is the production implementation.
//
// This is the only place the package shells out; keeping it behind an interface
// is what makes every per-OS argv path unit-testable from a single `go test`.
type runner interface {
	// look resolves name in PATH, returning its path or an error when absent.
	look(name string) (string, error)
	// run executes name with args, feeding stdin on the child's standard input
	// when non-empty, and blocks until it exits. It uses exec.CommandContext, so
	// cancelling ctx kills the child process (mid-playback for players).
	run(ctx context.Context, name, stdin string, args ...string) error
}

type execRunner struct{}

func (execRunner) look(name string) (string, error) { return exec.LookPath(name) }

func (execRunner) run(ctx context.Context, name, stdin string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}
