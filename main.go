// Command translate is a fast CLI/TUI translation tool.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"translate/cmd"
)

func main() {
	// Cancel in-flight work (including LLM requests) on Ctrl-C / SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := cmd.Execute(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "translate:", err)
		os.Exit(1)
	}
}
