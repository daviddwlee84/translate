package appcore

import (
	"strings"

	"github.com/daviddwlee84/translate/internal/debug"
	"github.com/daviddwlee84/translate/internal/engine"
	"github.com/daviddwlee84/translate/internal/lang"
	"github.com/daviddwlee84/translate/internal/store"
)

// EffectiveTarget applies pair mode: home-language input → away, else → home.
// It is the single source of truth for pair routing, shared by the one-shot CLI
// and the Service.
func EffectiveTarget(pair bool, home, away, text string) string {
	if pair && away != "" {
		t := lang.PairTarget(home, away, text)
		debug.Logf("pair route: home=%s away=%s text=%q → target=%s", home, away, clip(text, 30), t)
		return t
	}
	return home
}

// clip shortens a string for a single-line debug label.
func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// ToRecord builds a history Record from a translation result. When the source was
// "auto", the engine's detected language is substituted so history stays specific.
func ToRecord(res *engine.TranslateResult, input, source, target string) store.Record {
	src := source
	if src == "auto" && res.DetectedSource != "" {
		src = res.DetectedSource
	}
	return store.Record{
		SourceLang:   src,
		TargetLang:   target,
		Engine:       res.Engine,
		Model:        res.Model,
		Input:        input,
		Output:       res.Translation,
		Alternatives: res.Alternatives,
		Notes:        res.Notes,
	}
}
