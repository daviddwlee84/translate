package tts

import (
	"strings"

	"github.com/daviddwlee84/translate/internal/lang"
)

// Side selects which piece of a lookup/translation to speak.
type Side int

const (
	// SideAuto picks by the "foreign"/副 language preference (the default).
	SideAuto Side = iota
	// SideSource forces the input text (e.g. TUI input pane focused).
	SideSource
	// SideResult forces the translation / dictionary head word (result pane focused).
	SideResult
)

// SelectInput describes the current lookup/translation for speech-side selection.
type SelectInput struct {
	SourceText string // the user's input
	SourceLang string // resolved source code (may be "auto"/"")
	ResultText string // the translation, or the dictionary head word
	ResultLang string // effective target code
	Foreign    string // preferred "副"/away language; "" => derive (non-zh side)
	Forced     Side   // focus override; SideAuto elsewhere
}

// Choice is the resolved text and language to speak.
type Choice struct {
	Text string
	Lang string
}

// Select decides what to speak. ok is false when there is nothing to say.
//
// The core rule (from the user): speak the secondary/foreign ("副") language —
// for a zh-native user that is usually the non-Chinese side. A Forced side (the
// TUI's tab-focus) overrides, falling back to Auto only when that side is empty.
func Select(in SelectInput) (Choice, bool) {
	src := strings.TrimSpace(in.SourceText)
	res := strings.TrimSpace(in.ResultText)
	sl := langOf(src, in.SourceLang)
	rl := langOf(res, in.ResultLang)

	switch in.Forced {
	case SideSource:
		if src != "" {
			return Choice{Text: src, Lang: sl}, true
		}
	case SideResult:
		if res != "" {
			return Choice{Text: res, Lang: rl}, true
		}
	}

	foreign := lang.Base(normKey(in.Foreign))
	if foreign == "" || foreign == "auto" {
		foreign = deriveForeign(sl, rl)
	}

	// Prefer the side whose language matches the foreign preference; else default
	// to the result, then the source. Guard against an empty chosen side.
	switch {
	case res != "" && lang.Base(rl) == foreign:
		return Choice{Text: res, Lang: rl}, true
	case src != "" && lang.Base(sl) == foreign:
		return Choice{Text: src, Lang: sl}, true
	case res != "":
		return Choice{Text: res, Lang: rl}, true
	case src != "":
		return Choice{Text: src, Lang: sl}, true
	}
	return Choice{}, false
}

// deriveForeign guesses the away/副 language as the non-Chinese side (the user is
// zh-native). With neither or both Chinese it defaults to the result side.
func deriveForeign(sl, rl string) string {
	sb, rb := lang.Base(sl), lang.Base(rl)
	switch {
	case sb == "zh" && rb != "zh":
		return rb
	case rb == "zh" && sb != "zh":
		return sb
	case rb != "":
		return rb
	default:
		return sb
	}
}

// langOf resolves the language to speak text in. A Han-script check wins (keeping
// the region from the hint, e.g. zh-TW, since detection can't recover it), else
// offline detection, else the hint.
func langOf(text, hint string) string {
	h := normKey(hint)
	if strings.TrimSpace(text) == "" {
		return h
	}
	if lang.IsChinese(text) {
		if lang.Base(h) == "zh" {
			return h // preserve zh-TW / zh-HK for the voice
		}
		return "zh"
	}
	if d := lang.Detect(text); d != "" {
		return d
	}
	if h == "auto" {
		return ""
	}
	return h
}
