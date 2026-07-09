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

// PairTarget implements bidirectional "pair" mode: if the input is written in
// the home language, translate it to away; otherwise translate it to home. This
// makes "foreign → my language, my language → foreign" work from one input box
// (e.g. home=zh-TW, away=en: Chinese → en, everything else → zh-TW).
func PairTarget(home, away, text string) string {
	if away == "" || away == home {
		return home
	}
	if inLang(text, home) {
		return away
	}
	return home
}

// inLang reports whether text is (best-effort) in the language of code. Chinese
// uses a reliable Han-script check; others fall back to offline detection.
func inLang(text, code string) bool {
	base := baseCode(code)
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

// baseCode strips a region suffix: "zh-TW" → "zh".
func baseCode(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if i := strings.IndexByte(code, '-'); i > 0 {
		return code[:i]
	}
	return code
}
