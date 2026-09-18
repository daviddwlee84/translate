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

func TestBuildTranslatePromptTableDirective(t *testing.T) {
	// The reported failure: a results table translated into zh-TW, whose
	// space-padded alignment cannot survive either the translation (CJK is
	// double-width) or a proportional renderer.
	table := "| Model | V1 | V2 |\n" +
		"| dense-95 | 0.1210 | 0.0679 |\n" +
		"| dense-250 | 0.1208 | 0.0826 |"
	req := Request{Text: table, Source: "auto", Target: "zh-TW", Preset: PresetConcise}
	sys, _ := buildTranslatePrompt(req)

	if !strings.Contains(sys, "markdown table") {
		t.Errorf("tabular input did not get the table directive:\n%s", sys)
	}
	// The load-bearing half: structure yes, padding no.
	if !strings.Contains(sys, "Do NOT pad cells") {
		t.Errorf("table directive must forbid alignment padding:\n%s", sys)
	}
	// Appended, not substituted — the preset's own rules must survive.
	if !strings.Contains(sys, "professional translation engine") {
		t.Errorf("table directive replaced the preset instead of appending to it:\n%s", sys)
	}
}

func TestBuildTranslatePromptNoTableDirectiveForProse(t *testing.T) {
	req := Request{
		Text:   "Hello there.\nThis is ordinary prose.\nNothing tabular here.",
		Source: "auto", Target: "zh-TW", Preset: PresetConcise,
	}
	sys, _ := buildTranslatePrompt(req)
	if strings.Contains(sys, "markdown table") {
		t.Errorf("prose should not pay for the table directive:\n%s", sys)
	}
}

func TestBuildDocumentPromptReplacesPreset(t *testing.T) {
	text := "\n    printf 'hello'\n\n# Report\n\n- Preserve `--flag` and [the link](https://example.com).  \n\n" +
		"| Name | Value |\n| --- | --- |\n| Count | 3 |\n\nAfter the table.\n"
	for _, preset := range []string{PresetConcise, PresetContextual, PresetDictionary} {
		t.Run(preset, func(t *testing.T) {
			req := Request{
				Text: text, Source: "auto", Target: "zh-TW", Preset: preset,
				PreserveFormat: true, Extra: "Use Taiwanese terminology.",
			}
			sys, user := buildTranslatePrompt(req)
			for _, unwanted := range []string{
				"Output a SHORT list of 2-4 lines", "Then a blank line, then 1-2 example sentences",
				"The source text is a table.", "never echo it back",
			} {
				if strings.Contains(sys, unwanted) {
					t.Errorf("document prompt retains conflicting instruction %q", unwanted)
				}
			}
			for _, want := range []string{
				"Translate ALL natural-language prose", "Do not\n  summarize", "Markdown heading levels",
				"inline code", "file paths", "URLs", "identifiers", "numeric values",
				"preserve all surrounding prose", "Use Taiwanese terminology.",
			} {
				if !strings.Contains(sys, want) {
					t.Errorf("document prompt missing preservation requirement %q", want)
				}
			}
			if !strings.HasSuffix(user, text) {
				t.Errorf("document user prompt lost input whitespace: %q", user)
			}
		})
	}
}

func TestBuildDocumentPromptPairKeepsProtectedLiterals(t *testing.T) {
	req := Request{
		Text: "# Test\n\n`echo test`\n", Source: "auto", Target: "zh-TW",
		PreserveFormat: true, Pair: true, PairHome: "zh-TW", PairAway: "en",
	}
	sys, _ := buildTranslatePrompt(req)
	for _, code := range []string{req.PairHome, req.PairAway} {
		if !strings.Contains(sys, lang.Name(code)) {
			t.Errorf("document pair prompt missing language %q", code)
		}
	}
	if strings.Contains(sys, "never return the text unchanged") {
		t.Error("document pair prompt would force changes to code-only input")
	}
	if !strings.Contains(sys, "Detect its language from the prose") ||
		!strings.Contains(sys, "Keep code and all protected literals verbatim") {
		t.Errorf("document pair prompt lost prose routing or literal protection: %s", sys)
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
