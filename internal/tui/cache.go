package tui

import "github.com/daviddwlee84/translate/internal/engine"

// cacheKey identifies a translation result for the session cache. It is
// comparable, so it can be a map key. Keying on the *selected* engine name
// (e.g. "auto") means a chain fallback still caches under the user-visible engine.
// learn/pair/pairWith are included so a learn result, a pair result, and a plain
// translation of the same text never collide.
type cacheKey struct {
	preset, engineName, model, source, target, text string
	learn, pair                                     bool
	pairWith                                        string
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
		learn:      m.learn,
		pair:       m.pair,
		pairWith:   m.pairWith,
	}
}

// cacheStore is the session result cache. Session-scoped and unbounded (small);
// LLM output is nondeterministic so it is not persisted across sessions.
type cacheStore map[cacheKey]*engine.TranslateResult
