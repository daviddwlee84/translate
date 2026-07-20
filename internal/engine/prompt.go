package engine

import (
	"fmt"
	"strings"

	"github.com/daviddwlee84/translate/internal/lang"
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
- ALWAYS translate into the target language. A single word, name, technical term, or
  loanword written in a DIFFERENT language must still be translated — never echo it back.
  Only when the text is ALREADY in the target language should you return it (lightly
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

// pairDirective is appended to the system prompt in bidirectional pair mode. It
// makes the model own language detection (more robust than a pre-computed target
// for short/ambiguous input) and forbids echoing the input — the exact failure
// this fixes ("test" → "test" instead of "測試").
const pairDirective = `Bidirectional mode: the text is written in ONE of %s or %s. Detect which of the two it is, and translate it into the OTHER one. ALWAYS translate — never return the text unchanged, even for a single word, a proper name, a technical term, or a loanword.`

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
	// In pair mode, let the model detect the input language and route to the other
	// side, and never echo — this is appended to (not a replacement of) the preset
	// so the chosen output format (concise/contextual/dictionary) is preserved.
	if req.Pair && req.PairHome != "" && req.PairAway != "" {
		system += "\n\n" + fmt.Sprintf(pairDirective, lang.Name(req.PairHome), lang.Name(req.PairAway))
	}
	if extra := strings.TrimSpace(req.Extra); extra != "" {
		system += "\n\nUser preferences (apply where relevant, e.g. domain terminology):\n" + extra
	}
	return system, fmt.Sprintf(translateUserPrompt, src, tgt, req.Text)
}

// translateBilingualSystemPrompt drives context-aware whole-document ("doc mode")
// translation. The model sees the FULL document (prose + code as context) and
// returns a JSON object mapping each prose segment's number to its translation, so
// it can use cross-segment context — e.g. that a bare `rg` is the command being
// documented, not an abbreviation — and stay in one consistent target variant.
// %[1]s is the readable target language name (used twice).
const translateBilingualSystemPrompt = `You translate terminal / CLI documentation into %[1]s.

You are given the whole document as a numbered list of SEGMENTS. Some are prose to
translate; others are command/code lines marked "[code — context only]" and are
shown ONLY so you understand the document — never translate or echo them.

Rules:
- Use the WHOLE document as context. A bare token like "rg" or "ls" on its own is
  the COMMAND being documented, not an abbreviation — do not expand or gloss it.
- Keep command names, subcommands, flags, file paths, URLs, and code verbatim.
- Preserve meaning, tone, and register. Interpret the intended meaning of typos; never refuse.
- Translate EVERY prose segment into %[1]s only — one consistent variant, never mixed.
- Respond with ONE JSON object and NOTHING else: keys are the prose segment numbers
  (as strings), values are the translations. No markdown fence, no reasoning, no
  commentary before or after the JSON.`

// bilingualUserPrompt frames the numbered document for a doc-mode request.
const bilingualUserPrompt = `Source language: %s
Target language: %s

Document segments:
%s`

// buildBilingualPrompt returns the (system, user) messages for a doc-mode bilingual
// request. Prose segments are numbered (1-based, matching TranslateResult.Bilingual
// keys); code segments are shown inline as context and are NOT numbered.
func buildBilingualPrompt(req Request) (system, user string) {
	src := strings.TrimSpace(req.Source)
	if src == "" || src == "auto" {
		src = "auto"
	} else {
		src = fmt.Sprintf("%s (%s)", lang.Name(src), src)
	}
	tgt := fmt.Sprintf("%s (%s)", lang.Name(req.Target), req.Target)
	system = fmt.Sprintf(translateBilingualSystemPrompt, tgt)
	if extra := strings.TrimSpace(req.Extra); extra != "" {
		system += "\n\nUser preferences (apply where relevant, e.g. domain terminology):\n" + extra
	}

	var b strings.Builder
	n := 0
	for _, s := range req.Segments {
		text := strings.TrimSpace(s.Text)
		if s.Code {
			b.WriteString("    [code — context only] " + text + "\n")
			continue
		}
		n++
		fmt.Fprintf(&b, "%d. %s\n", n, text)
	}
	return system, fmt.Sprintf(bilingualUserPrompt, src, tgt, b.String())
}

// learnDirection reports the learn-mode direction, decided OFFLINE via the same
// script-aware router as pair mode (lang.PairTarget): native (home) input → "teach"
// (translate + glosses); foreign (away) input → "correct" (grammar-correct + explain).
// Trusting this over the model's own guess keeps the two output shapes deterministic.
func learnDirection(req Request) string {
	if lang.PairTarget(req.PairHome, req.PairAway, req.Text) == req.PairAway {
		return "teach"
	}
	return "correct"
}

// learnTeachPrompt drives native→foreign tutoring. %[1]s = native (home) language,
// %[2]s = foreign (away) language being learned.
const learnTeachPrompt = `You are a warm, encouraging language tutor helping a native %[1]s speaker learn %[2]s.
The user writes in %[1]s. Translate their text into natural, idiomatic %[2]s, then teach it.

Respond with ONE JSON object and NOTHING else — no markdown code fence, no prose before or after it.
Schema (fill every relevant field; omit a field only when it genuinely has no content):
{
  "direction": "teach",
  "original": "<the user's %[1]s input, verbatim>",
  "translation": "<the idiomatic %[2]s translation>",
  "vocab": [
    {"term": "<key %[2]s word or phrase>", "pos": "<part of speech>", "phonetic": "<KK/IPA for English, pinyin for Chinese>", "meaning": "<concise meaning in %[1]s>"}
  ],
  "examples": [
    {"foreign": "<a short natural %[2]s sentence using the translation>", "native": "<its %[1]s translation>"}
  ],
  "notes": "<one short usage or register tip in %[1]s, optional>"
}

Rules:
- Write every meaning, note, and explanation in %[1]s (the learner's language).
- vocab: only the key content words/phrases worth learning (at most 8); skip trivial function words.
- examples: 1-2, short and idiomatic.
- Interpret the intended meaning of typos/slang; never refuse. Output ONLY the JSON object.`

// learnCorrectPrompt drives foreign→native correction. %[1]s = foreign (away)
// language being learned, %[2]s = native (home) language.
const learnCorrectPrompt = `You are a warm, encouraging language tutor helping a %[2]s speaker who is learning %[1]s.
The user writes a sentence in %[1]s that may contain mistakes. Correct it and explain, kindly.

Respond with ONE JSON object and NOTHING else — no markdown code fence, no prose before or after it.
Schema (fill every relevant field; omit a field only when it genuinely has no content):
{
  "direction": "correct",
  "original": "<the user's %[1]s input, verbatim>",
  "corrected": "<the grammatically correct, idiomatic %[1]s sentence>",
  "translation": "<a %[2]s translation of the intended meaning>",
  "issues": [
    {"span": "<the problematic fragment of the input>", "fix": "<the corrected fragment>", "explanation": "<why, in %[2]s>"}
  ],
  "notes": "<one short encouraging tip in %[2]s, optional>"
}

Rules:
- Write every explanation and note in %[2]s (the learner's native language).
- If the sentence is already correct, echo it in "corrected" and return "issues": [].
- List each distinct mistake as one issue; keep explanations concise and beginner-friendly.
- Interpret the intended meaning of typos/slang; never refuse. Output ONLY the JSON object.`

// learnUserPrompt frames the concrete learn request. The system prompt already
// names both languages, so the user turn carries only the text.
const learnUserPrompt = `Text:
%s`

// buildLearnPrompt returns the (system, user) messages for a learn-mode request,
// choosing the teach or correct prompt from the offline-detected direction. Codes
// are expanded to readable names so regional variants (zh-TW) read naturally.
func buildLearnPrompt(req Request) (system, user string) {
	home := lang.Name(req.PairHome) // native
	away := lang.Name(req.PairAway) // foreign
	if learnDirection(req) == "teach" {
		system = fmt.Sprintf(learnTeachPrompt, home, away)
	} else {
		system = fmt.Sprintf(learnCorrectPrompt, away, home)
	}
	if extra := strings.TrimSpace(req.Extra); extra != "" {
		system += "\n\nUser preferences (apply where relevant, e.g. domain terminology):\n" + extra
	}
	return system, fmt.Sprintf(learnUserPrompt, req.Text)
}
