package engine

import (
	"fmt"
	"strings"
)

// translateSystemPrompt drives the primary (hot-path) translation. It asks for
// ONLY the translated text — no JSON, no commentary — so the reply streams
// token-by-token identically across every OpenAI-compatible backend and is
// latency-optimal. Richer output (alternatives/notes/confidence) is a separate,
// optional enrichment pass, not part of this stream.
const translateSystemPrompt = `You are a precise, professional translation engine.
Translate the user's text into the target language faithfully and idiomatically.

Rules:
- Preserve the meaning, tone, and register (formal/informal, technical/casual) of the source.
- The user's text may contain typos, slang, or ungrammatical phrasing. Interpret their
  INTENDED meaning and translate that; do not translate a typo literally, and never refuse.
- Output ONLY the translated text. No quotes, no explanations, no language labels,
  no commentary before or after.
- If the source language equals the target language, return the text unchanged (lightly
  corrected if it was misspelled).`

// translateUserPrompt frames the concrete request.
const translateUserPrompt = `Source language: %s
Target language: %s

Text:
%s`

// buildTranslatePrompt returns the (system, user) messages for a plain-text
// translation request.
func buildTranslatePrompt(req Request) (system, user string) {
	src := strings.TrimSpace(req.Source)
	if src == "" {
		src = "auto"
	}
	return translateSystemPrompt, fmt.Sprintf(translateUserPrompt, src, req.Target, req.Text)
}
