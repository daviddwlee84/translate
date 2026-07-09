package lang

import (
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
