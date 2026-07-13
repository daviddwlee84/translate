package engine

import (
	"context"
	"strings"
	"unicode"

	"github.com/daviddwlee84/translate/internal/debug"
	"github.com/daviddwlee84/translate/internal/lang"
)

// SmartAutoEngine is the "smart" default: it treats a single word/term as a
// dictionary lookup (delegating to the smart-dict engine, which itself falls back
// to the LLM on a miss) and a phrase or sentence as an LLM translation. It is
// bidirectional-friendly — the caller sets Pair/PairHome/PairAway and the routed
// Target, and both branches honor them.
//
// Because dictionary word lookups are direction-agnostic (script routes them to
// CC-CEDICT vs ECDICT) and phrase translation uses the pair-aware LLM prompt, a
// single word like "test" is answered from the dictionary instead of risking an
// LLM echo, and a full sentence is translated into the other pair language.
type SmartAutoEngine struct {
	smart Engine // dictionary + LLM fallback (serves ModeDict)
	llm   Engine // LLM translate, possibly a fallback chain (serves ModeTranslate)
}

// NewSmartAuto builds a smart-auto engine over a smart-dict engine (word lookups)
// and an LLM/translate engine (phrases).
func NewSmartAuto(smart, llm Engine) *SmartAutoEngine {
	return &SmartAutoEngine{smart: smart, llm: llm}
}

// Name returns "smart-auto".
func (e *SmartAutoEngine) Name() string { return "smart-auto" }

// Supports translate mode (the main-path request mode); it dispatches to a
// dictionary lookup internally when the input looks like a single word.
func (e *SmartAutoEngine) Supports(m Mode) bool { return m == ModeTranslate }

// Detect is not meaningful here (the LLM/dict sub-engines own detection).
func (e *SmartAutoEngine) Detect(ctx context.Context, text string) (string, error) {
	return "", nil
}

// Available reports true when either branch can serve.
func (e *SmartAutoEngine) Available(ctx context.Context) bool {
	return e.smart.Available(ctx) || e.llm.Available(ctx)
}

// Translate routes a single word/term to the dictionary (smart-dict) and a phrase
// to the LLM. Streaming is preserved by returning the chosen sub-engine's channel.
func (e *SmartAutoEngine) Translate(ctx context.Context, req Request) (<-chan Chunk, error) {
	if isLookup(req.Text) {
		debug.Logf("smart-auto: %q → dictionary lookup (smart-dict), target=%s", truncateText(req.Text), req.Target)
		r := req
		r.Mode = ModeDict
		return e.smart.Translate(ctx, r)
	}
	debug.Logf("smart-auto: %q → LLM translate, target=%s pair=%v", truncateText(req.Text), req.Target, req.Pair)
	r := req
	r.Mode = ModeTranslate
	return e.llm.Translate(ctx, r)
}

// isLookup reports whether text should be treated as a dictionary lookup (a
// single word or short term) rather than a phrase to translate. Latin/other: one
// token of letters (plus '-' / apostrophe), up to 32 runes. CJK: a short run of
// Han without spaces (a word or idiom, up to 4 characters). Anything with
// internal whitespace is a phrase.
func isLookup(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	if strings.ContainsAny(t, " \t\n\r") {
		return false // multiple tokens → phrase
	}
	if lang.IsChinese(t) {
		return len([]rune(t)) <= 4
	}
	for _, r := range t {
		if !unicode.IsLetter(r) && r != '-' && r != '\'' {
			return false // digits/punctuation → not a plain word
		}
	}
	return len([]rune(t)) <= 32
}

// truncateText shortens a string for a single-line debug label.
func truncateText(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > 40 {
		return string(r[:40]) + "…"
	}
	return s
}
