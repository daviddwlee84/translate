package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/daviddwlee84/translate/internal/engine"
)

// writeResultDiagnostics keeps setup hints and fallback warnings out of stdout,
// including when stdout is a JSON document or input to another program.
func writeResultDiagnostics(w io.Writer, res *engine.TranslateResult) {
	if res == nil {
		return
	}
	seen := make(map[string]bool)
	if note := strings.TrimSpace(res.Notes); note != "" {
		fmt.Fprintf(w, "translate: %s\n", note)
		seen[note] = true
	}
	for _, warning := range res.Warnings {
		warning = strings.TrimSpace(warning)
		if warning != "" && !seen[warning] {
			fmt.Fprintf(w, "translate: warning: %s\n", warning)
			seen[warning] = true
		}
	}
}
