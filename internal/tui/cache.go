package tui

import "translate/internal/engine"

// cacheKey identifies a translation result for the session cache. It is
// comparable, so it can be a map key. Keying on the *selected* engine name
// (e.g. "auto") means a chain fallback still caches under the user-visible engine.
type cacheKey struct {
	preset, engineName, model, source, target, text string
}

// cacheKeyFor builds the key for the given input text under the current settings.
func (m Model) cacheKeyFor(text string) cacheKey {
	return cacheKey{
		preset:     m.preset,
		engineName: m.active().Name,
		model:      m.modelOverride,
		source:     m.source,
		target:     m.target,
		text:       text,
	}
}

// cacheStore is the session result cache. Session-scoped and unbounded (small);
// LLM output is nondeterministic so it is not persisted across sessions.
type cacheStore map[cacheKey]*engine.TranslateResult
