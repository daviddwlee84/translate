package tts

import (
	"strings"

	"github.com/daviddwlee84/translate/internal/lang"
)

// sayVoices maps a language code to a macOS `say` voice. Region-specific codes
// (zh-TW) are consulted before the base code (zh). "" lets `say` pick its default.
var sayVoices = map[string]string{
	"en":    "Samantha",
	"zh":    "Tingting",
	"zh-cn": "Tingting",
	"zh-tw": "Meijia",
	"zh-hk": "Sinji",
	"ja":    "Kyoko",
	"ko":    "Yuna",
	"fr":    "Thomas",
	"es":    "Monica",
	"de":    "Anna",
	"it":    "Alice",
	"ru":    "Milena",
	"pt":    "Luciana",
	"nl":    "Xander",
}

// espeakVoices maps a language code to an espeak-ng voice/language name.
var espeakVoices = map[string]string{
	"zh":    "cmn",
	"zh-cn": "cmn",
	"zh-tw": "cmn",
	"zh-hk": "yue",
	"ja":    "ja",
	"ko":    "ko",
}

// normKey lower-cases and trims a language code for map lookups.
func normKey(code string) string { return strings.ToLower(strings.TrimSpace(code)) }

// lookupVoice resolves code against an override map first, then a built-in map,
// trying the exact code before the base code. Returns "" when nothing matches.
func lookupVoice(builtin, override map[string]string, code string) string {
	k := normKey(code)
	base := lang.Base(k)
	for _, m := range []map[string]string{override, builtin} {
		if m == nil {
			continue
		}
		if v, ok := m[k]; ok {
			return v
		}
		if base != k {
			if v, ok := m[base]; ok {
				return v
			}
		}
	}
	return ""
}

// sayVoice returns the macOS `say` voice for code (override wins), or "" for the
// system default.
func sayVoice(override map[string]string, code string) string {
	return lookupVoice(sayVoices, override, code)
}

// espeakVoice returns the espeak-ng voice/language for code, falling back to the
// base code so espeak still attempts a matching language.
func espeakVoice(override map[string]string, code string) string {
	if v := lookupVoice(espeakVoices, override, code); v != "" {
		return v
	}
	return lang.Base(normKey(code))
}

// googleTL normalizes a language code to a Google translate_tts `tl` value.
func googleTL(code string) string {
	switch normKey(code) {
	case "zh", "zh-cn":
		return "zh-CN"
	case "zh-tw", "zh-hk":
		return "zh-TW"
	}
	if b := lang.Base(normKey(code)); b != "" {
		return b
	}
	return "en"
}
