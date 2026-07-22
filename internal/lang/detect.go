package lang

import (
	"strings"
	"unicode"

	"github.com/abadojack/whatlanggo"
)

// Detect returns a best-effort ISO-639-1 source-language code for text, or "" if
// the guess is not confident. Used to fill DetectedSource on the LLM path (where
// the model returns only the translation) and as a fallback when APIs are down.
func Detect(text string) string {
	info := whatlanggo.Detect(text)
	if info.Confidence < 0.5 {
		return ""
	}
	return info.Lang.Iso6391()
}

// IsChinese reports whether text contains any Han characters — used to route
// dictionary lookups (Chinese → CC-CEDICT, else → ECDICT).
func IsChinese(text string) bool {
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// isCJKRune reports whether r is a CJK-script rune (Han, Hiragana, Katakana, or
// Hangul) — one such rune is roughly a word's worth of content.
func isCJKRune(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r)
}

// containsCJK reports whether text contains any CJK-script rune (Han, Hiragana,
// Katakana, or Hangul). This is a reliable, offline signal for routing a
// CJK/non-CJK language pair — unlike trigram detection, it is confident even on
// very short input.
func containsCJK(text string) bool {
	for _, r := range text {
		if isCJKRune(r) {
			return true
		}
	}
	return false
}

// cjkDominant reports whether CJK script dominates text, weighing content rather
// than mere presence: it compares the CJK rune count against the number of
// non-CJK "words" (maximal runs of non-CJK letters, any script). Because one CJK
// rune is roughly a word, this keeps a few CJK proper nouns embedded in a long
// Latin passage from flipping the routing — the bug where a mostly-English
// paragraph containing "李榮浩" was treated as Chinese and "translated" back into
// English — while genuinely CJK-heavy text still counts as CJK.
func cjkDominant(text string) bool {
	var cjk, otherWords int
	inWord := false
	for _, r := range text {
		switch {
		case isCJKRune(r):
			cjk++
			inWord = false
		case unicode.IsLetter(r):
			if !inWord {
				otherWords++
				inWord = true
			}
		default:
			inWord = false
		}
	}
	return cjk > otherWords
}

// isCJKLang reports whether a language code is Chinese, Japanese, or Korean.
func isCJKLang(code string) bool {
	switch Base(code) {
	case "zh", "ja", "ko":
		return true
	}
	return false
}

// PairTarget implements bidirectional "pair" mode: detect which of the two pair
// languages the text is written in and return the OTHER one (so "foreign → my
// language, my language → foreign" both work from one input box).
//
// When exactly one side of the pair is a CJK language, routing is decided by
// which script dominates the text — reliable and offline, and symmetric
// regardless of which side is "home". Dominance (not mere presence) is what lets
// short Latin input like "test" route to the CJK side, while a few CJK proper
// nouns inside a long Latin passage stay routed to the CJK side too (rather than
// flipping the whole thing to Latin). Same-script pairs (e.g. en⇄es) fall back to
// best-effort trigram detection.
func PairTarget(home, away, text string) string {
	if away == "" || away == home {
		return home
	}
	homeCJK, awayCJK := isCJKLang(home), isCJKLang(away)
	if homeCJK != awayCJK {
		cjk, latin := home, away
		if awayCJK {
			cjk, latin = away, home
		}
		if cjkDominant(text) {
			return latin // predominantly CJK → the non-CJK language
		}
		return cjk // predominantly non-CJK → the CJK language
	}
	// Same-script pair (both or neither CJK): best-effort, symmetric detection.
	inHome, inAway := inLang(text, home), inLang(text, away)
	if inHome && !inAway {
		return away
	}
	if inAway && !inHome {
		return home
	}
	return home // inconclusive → default to home
}

// inLang reports whether text is (best-effort) in the language of code. Chinese
// uses a reliable Han-script check; others fall back to offline detection.
func inLang(text, code string) bool {
	base := Base(code)
	switch base {
	case "", "auto":
		return false
	case "zh":
		return IsChinese(text)
	default:
		d := Detect(text)
		return d != "" && d == base
	}
}

// Base strips a region suffix and normalizes case: "zh-TW" → "zh". It is the
// shared building block for region-insensitive language routing (used by the
// pair router here and by the tts voice/side selection).
func Base(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if i := strings.IndexByte(code, '-'); i > 0 {
		return code[:i]
	}
	return code
}
