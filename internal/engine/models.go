package engine

import (
	"regexp"
	"strings"
)

// datedSuffix matches trailing dated model-id forms like "-20250514" or
// "@2025-05-14" that the copilot-proxy rejects.
var datedSuffix = regexp.MustCompile(`(-\d{8}|@\d{4}-\d{2}-\d{2})$`)

// NormalizeModelID canonicalizes a model id for the copilot-proxy and other
// OpenAI-compatible backends:
//
//   - strips a trailing "[1m]" (or any "[...]") context-window hint, which is a
//     Claude-Code-only annotation the proxy rejects,
//   - drops a trailing dated suffix ("-YYYYMMDD" / "@YYYY-MM-DD"),
//   - trims surrounding whitespace.
//
// It leaves hyphenated ids like "claude-sonnet-5" and "gpt-5.4-mini" and Ollama
// ids like "llama3.2:3b" untouched.
func NormalizeModelID(id string) string {
	id = strings.TrimSpace(id)
	if i := strings.Index(id, "["); i >= 0 {
		id = strings.TrimSpace(id[:i])
	}
	id = datedSuffix.ReplaceAllString(id, "")
	return id
}

// ModelRec is a recommended model surfaced by `init` and `--help`.
type ModelRec struct {
	Role string // "default" | "fast" | "max" | "offline"
	ID   string
	Note string
}

// Recommended is the built-in model recommendation table (verified to work
// through copilot-proxy). Only models whose provider probes up are offered.
var Recommended = []ModelRec{
	{Role: "fast", ID: "gemini-3.5-flash", Note: "snappy; default for quick translations"},
	{Role: "default", ID: "claude-sonnet-5", Note: "balanced quality/speed"},
	{Role: "max", ID: "gpt-5.4", Note: "highest quality"},
	{Role: "offline", ID: "llama3.2:3b", Note: "Ollama, no network"},
}
