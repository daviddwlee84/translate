package engine

import (
	"strings"
	"testing"

	"github.com/daviddwlee84/translate/internal/lang"
)

func TestBuildTranslatePromptPairDirective(t *testing.T) {
	req := Request{
		Text:     "test",
		Source:   "auto",
		Target:   "zh-TW",
		Preset:   PresetConcise,
		Pair:     true,
		PairHome: "zh-TW",
		PairAway: "en",
	}
	sys, _ := buildTranslatePrompt(req)

	// The pair directive must name both languages so the model can detect + route.
	for _, code := range []string{"zh-TW", "en"} {
		if name := lang.Name(code); !strings.Contains(sys, name) {
			t.Errorf("pair system prompt missing language %q (%s)", code, name)
		}
	}
	// And it must forbid echoing the input unchanged (the "test → test" bug).
	if !strings.Contains(strings.ToLower(sys), "never return the text unchanged") {
		t.Errorf("pair system prompt is missing the no-echo instruction:\n%s", sys)
	}
}

func TestBuildTranslatePromptNoDirectiveWhenNotPair(t *testing.T) {
	req := Request{Text: "test", Source: "auto", Target: "zh-TW", Preset: PresetConcise}
	sys, _ := buildTranslatePrompt(req)
	if strings.Contains(sys, "Bidirectional mode") {
		t.Errorf("non-pair prompt should not include the pair directive:\n%s", sys)
	}
}

func TestConcisePromptForbidsEcho(t *testing.T) {
	// Even outside pair mode, the concise prompt must not tell the model to echo a
	// word in a different language.
	if strings.Contains(translateSystemPromptConcise, "return the text unchanged") &&
		!strings.Contains(translateSystemPromptConcise, "ALREADY in the target language") {
		t.Errorf("concise prompt still has a bare echo escape hatch:\n%s", translateSystemPromptConcise)
	}
}

func TestBuildBilingualPrompt(t *testing.T) {
	req := Request{
		Source:    "auto",
		Target:    "zh-TW",
		Bilingual: true,
		Segments: []Segment{
			{Text: "rg"},
			{Text: "Ripgrep, a recursive tool."},
			{Text: "rg pattern", Code: true},
		},
	}
	sys, user := buildBilingualPrompt(req)

	// System: context directive, JSON-only instruction, and the resolved target.
	for _, want := range []string{"not an abbreviation", "JSON", "zh-TW"} {
		if !strings.Contains(sys, want) {
			t.Errorf("bilingual system prompt missing %q:\n%s", want, sys)
		}
	}
	// User: prose numbered 1,2; code shown as context, never numbered.
	if !strings.Contains(user, "1. rg") || !strings.Contains(user, "2. Ripgrep") {
		t.Errorf("prose segments not numbered as expected:\n%s", user)
	}
	if !strings.Contains(user, "[code — context only] rg pattern") {
		t.Errorf("code segment not marked as context:\n%s", user)
	}
	if strings.Contains(user, "3. rg pattern") {
		t.Errorf("code segment must not be numbered for translation:\n%s", user)
	}
}
