package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/daviddwlee84/translate/internal/engine"
)

func TestWriteResultDiagnostics(t *testing.T) {
	var out bytes.Buffer
	writeResultDiagnostics(&out, &engine.TranslateResult{
		Translation: "translation stays on stdout", Notes: "install dictionary", Warnings: []string{"install dictionary", "fallback", "fallback", " "},
	})
	if got, want := out.String(), "translate: install dictionary\ntranslate: warning: fallback\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if strings.Contains(out.String(), "translation stays") {
		t.Fatal("rendered translation as a diagnostic")
	}
	if got := renderDict(&engine.TranslateResult{Notes: "missing"}); got != "" {
		t.Fatalf("note-only result polluted stdout: %q", got)
	}
	if got := renderDict(&engine.TranslateResult{Suggestions: []string{"test"}}); !strings.Contains(got, "test") {
		t.Fatalf("lost suggestion: %q", got)
	}
}
