package engine

import (
	"fmt"
	"strings"

	"translate/internal/lang"
)

// Preset selects the LLM translate prompt style. Only ModeTranslate LLM engines
// honor it; google/dict ignore it.
const (
	PresetConcise    = "concise"    // terse direct translation (default)
	PresetContextual = "contextual" // short list of common-sense translations
	PresetDictionary = "dictionary" // translation + 1-2 example sentences
)

// translateSystemPromptConcise drives the primary (hot-path) translation. It asks
// for ONLY the translated text so the reply streams token-by-token identically
// across every backend and is latency-optimal.
const translateSystemPromptConcise = `You are a precise, professional translation engine.
Translate the user's text into the target language faithfully and idiomatically.

Rules:
- Preserve the meaning, tone, and register (formal/informal, technical/casual) of the source.
- The user's text may contain typos, slang, or ungrammatical phrasing. Interpret their
  INTENDED meaning and translate that; do not translate a typo literally, and never refuse.
- Output ONLY the translated text. No quotes, no explanations, no language labels,
  no commentary before or after.
- If the source language equals the target language, return the text unchanged (lightly
  corrected if it was misspelled).`

// translateSystemPromptContextual lists translations across common senses.
const translateSystemPromptContextual = `You are a precise, professional translation engine.
Translate the user's text into the target language, showing how the translation changes
across the most common senses or contexts of the source.

Rules:
- Preserve meaning, tone, and register. The text may contain typos, slang, or ungrammatical
  phrasing — interpret the INTENDED meaning; never refuse and never translate a typo literally.
- Output a SHORT list of 2-4 lines, one line per distinct common sense/context.
- Format each line EXACTLY as: "N. <translation> — <short context label in the TARGET language>".
- Order lines from the most likely/common sense to the least.
- If the text has only one natural sense, output a single line — do not invent senses.
- Output ONLY the list. No preamble, no headings, no commentary before or after.`

// translateSystemPromptDictionary gives a translation plus example sentences.
const translateSystemPromptDictionary = `You are a precise, professional translation engine
that also gives brief usage examples.
Translate the user's text into the target language, then illustrate it with example sentences.

Rules:
- Preserve meaning, tone, and register. Interpret the INTENDED meaning of typos/slang; never refuse.
- The FIRST line is ONLY the translation of the user's text — no label, no quotes.
- Then a blank line, then 1-2 example sentences that use the translation naturally.
- Format each example on two lines:
    line 1: the example sentence in the TARGET language,
    line 2: its translation in the source language (the language of the user's input),
            prefixed with "  ↳ ".
- Keep examples short and idiomatic. Output ONLY the translation and the examples —
  no headings, no numbering of the first line, no commentary.`

// systemPromptFor returns the system prompt for a preset (defaults to concise).
func systemPromptFor(preset string) string {
	switch preset {
	case PresetContextual:
		return translateSystemPromptContextual
	case PresetDictionary:
		return translateSystemPromptDictionary
	default:
		return translateSystemPromptConcise
	}
}

// translateUserPrompt frames the concrete request.
const translateUserPrompt = `Source language: %s
Target language: %s

Text:
%s`

// buildTranslatePrompt returns the (system, user) messages for a plain-text
// translation request. Language codes are expanded to readable names so the
// model handles regional variants (e.g. zh-TW → "chinese (traditional)").
func buildTranslatePrompt(req Request) (system, user string) {
	src := strings.TrimSpace(req.Source)
	if src == "" || src == "auto" {
		src = "auto"
	} else {
		src = fmt.Sprintf("%s (%s)", lang.Name(src), src)
	}
	tgt := fmt.Sprintf("%s (%s)", lang.Name(req.Target), req.Target)
	system = systemPromptFor(req.Preset)
	if extra := strings.TrimSpace(req.Extra); extra != "" {
		system += "\n\nUser preferences (apply where relevant, e.g. domain terminology):\n" + extra
	}
	return system, fmt.Sprintf(translateUserPrompt, src, tgt, req.Text)
}
