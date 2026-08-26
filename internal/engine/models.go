package engine

import (
	"regexp"
	"sort"
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

// Recommended is the built-in model recommendation table. Claude models route
// through the Anthropic Messages API automatically. Only models whose provider
// probes up are offered.
var Recommended = []ModelRec{
	{Role: "fast", ID: "claude-haiku-4-5", Note: "snappy; default for quick translations"},
	{Role: "default", ID: "claude-sonnet-5", Note: "balanced quality/speed"},
	{Role: "max", ID: "claude-opus-4-8", Note: "highest quality"},
	{Role: "offline", ID: "llama3.2:3b", Note: "Ollama, no network"},
}

// preferredMaxModels mirrors the quality-first ordering used by this machine's
// `copilot-model --auto`. The fast/default tables preserve translate's tier
// contract instead of sending every request to the most expensive model.
var preferredMaxModels = []string{
	"claude-fable-5",
	"claude-opus-5", "claude-opus-4-8", "claude-opus-4-7", "claude-opus-4-6",
	"claude-sonnet-5", "claude-sonnet-4-6", "claude-sonnet-4-5",
	"claude-opus-4-5", "claude-haiku-4-5",
	"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.5", "gpt-5.4", "gpt-5.3-codex",
	"gpt-5.6-luna", "gpt-5.4-mini", "gpt-5-mini",
}

var preferredDefaultModels = []string{
	"claude-sonnet-5", "claude-sonnet-4-6", "claude-sonnet-4-5",
	"gpt-5.6-terra", "gpt-5.5", "gpt-5.4", "gpt-5.3-codex", "gpt-5.6-sol",
	"gemini-3.1-pro-preview",
}

var preferredFastModels = []string{
	"claude-haiku-4-5",
	"gpt-5.6-luna", "gpt-5.4-mini", "gpt-5-mini",
	"gemini-3.7-flash", "gemini-3.6-flash", "gemini-3.5-flash",
}

// pickBestModel selects the strongest model from a live, already-filtered
// provider catalog. It deliberately returns a raw API id (never a Claude Code
// context annotation such as "[1m]").
func pickBestModel(models []string, tier string) string {
	set := make(map[string]string, len(models))
	for _, model := range models {
		raw := NormalizeModelID(model)
		if raw != "" {
			set[strings.ToLower(raw)] = raw
		}
	}
	if len(set) == 0 {
		return ""
	}
	preferredModels := preferredDefaultModels
	switch tier {
	case "fast":
		preferredModels = preferredFastModels
	case "max":
		preferredModels = preferredMaxModels
	}
	for _, preferred := range preferredModels {
		if model := set[preferred]; model != "" {
			return model
		}
	}
	// If the selected tier has no known representative, fall back to the full
	// quality ordering rather than choosing an arbitrary catalog entry.
	for _, preferred := range preferredMaxModels {
		if model := set[preferred]; model != "" {
			return model
		}
	}

	values := make([]string, 0, len(set))
	for _, model := range set {
		values = append(values, model)
	}
	sort.Strings(values)

	// Unknown future model ids retain the same family ordering as the explicit
	// table. Within a family, the lexicographically newest spelling wins.
	for _, match := range []func(string) bool{
		func(s string) bool { return strings.HasPrefix(s, "claude-") },
		func(s string) bool {
			return strings.HasPrefix(s, "gpt-") &&
				!strings.Contains(s, "mini") && !strings.Contains(s, "nano") && !strings.Contains(s, "luna")
		},
		func(s string) bool { return strings.Contains(s, "codex") },
		func(s string) bool { return strings.HasPrefix(s, "gpt-") },
		func(s string) bool { return strings.HasPrefix(s, "gemini-") && !strings.Contains(s, "flash") },
		func(s string) bool { return strings.HasPrefix(s, "gemini-") },
	} {
		for i := len(values) - 1; i >= 0; i-- {
			if match(strings.ToLower(values[i])) {
				return values[i]
			}
		}
	}
	return values[len(values)-1]
}
