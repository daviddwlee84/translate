package engine

import (
	"context"
	"fmt"
	"testing"
)

// fakeEngine is a scriptable Engine for testing the smart-dict composition: it
// emits one terminal chunk (res, or err) and records the request it received.
type fakeEngine struct {
	name   string
	res    *TranslateResult
	err    error
	called bool
	gotReq Request
}

func (f *fakeEngine) Name() string                                            { return f.name }
func (f *fakeEngine) Supports(m Mode) bool                                    { return true }
func (f *fakeEngine) Detect(ctx context.Context, text string) (string, error) { return "", nil }
func (f *fakeEngine) Available(ctx context.Context) bool                      { return true }
func (f *fakeEngine) Translate(ctx context.Context, req Request) (<-chan Chunk, error) {
	f.called = true
	f.gotReq = req
	return single(f.res, f.err), nil
}

// The smart-dict engine falls back to the LLM only when the dictionary misses or
// the fuzzy match is too far off; exact hits and close typos pass through untouched.
func TestSmartDictFallbackDecision(t *testing.T) {
	hit := &TranslateResult{Translation: "gloss", Dictionary: &DictEntry{Word: "cat"}, Engine: "dictionary"}
	near := &TranslateResult{Suggestions: []string{"cat"}, SuggestDistance: 1, Engine: "dictionary"}
	far := &TranslateResult{Suggestions: []string{"cot"}, SuggestDistance: 2, Engine: "dictionary"}
	zhPrefix := &TranslateResult{Suggestions: []string{"貓咪"}, SuggestDistance: 0, Engine: "dictionary"} // unknown distance

	cases := []struct {
		name     string
		dictRes  *TranslateResult
		dictErr  error
		wantLLM  bool
		wantText string // "" => don't assert (pass-through result has no translation)
		wantWarn bool
	}{
		{"exact-hit", hit, nil, false, "gloss", false},
		{"near-typo", near, nil, false, "", false},
		{"far-fuzzy", far, nil, true, "LLM answer", true},
		{"zh-prefix-unknown-distance", zhPrefix, nil, true, "LLM answer", true},
		{"hard-miss", nil, fmt.Errorf("dictionary: %w: %q", ErrNoDictEntry, "zzz"), true, "LLM answer", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dict := &fakeEngine{name: "dictionary", res: tc.dictRes, err: tc.dictErr}
			llm := &fakeEngine{name: "fakellm", res: &TranslateResult{Translation: "LLM answer", Engine: "fakellm"}}
			e := NewSmartDict(dict, llm, SmartDictConfig{CloseDistance: 1})

			ch, err := e.Translate(context.Background(), Request{Text: "cat", Target: "en", Mode: ModeDict})
			if err != nil {
				t.Fatalf("Translate: %v", err)
			}
			res, derr := Drain(ch, nil)
			if derr != nil {
				t.Fatalf("Drain: %v", derr)
			}
			if llm.called != tc.wantLLM {
				t.Errorf("llm.called = %v, want %v", llm.called, tc.wantLLM)
			}
			if tc.wantText != "" && res.Translation != tc.wantText {
				t.Errorf("Translation = %q, want %q", res.Translation, tc.wantText)
			}
			if hasWarn := len(res.Warnings) > 0; hasWarn != tc.wantWarn {
				t.Errorf("warnings = %v, want warn=%v", res.Warnings, tc.wantWarn)
			}
		})
	}
}

// On fallback the LLM is driven in translate mode with the dictionary preset and a
// target that mirrors the bilingual dictionary (English word, target en → Chinese).
func TestSmartDictFallbackRequest(t *testing.T) {
	dict := &fakeEngine{name: "dictionary", err: fmt.Errorf("dictionary: %w: %q", ErrNoDictEntry, "hello")}
	llm := &fakeEngine{name: "fakellm", res: &TranslateResult{Translation: "你好", Engine: "fakellm"}}
	e := NewSmartDict(dict, llm, SmartDictConfig{CloseDistance: 1}) // empty Preset => PresetDictionary

	ch, err := e.Translate(context.Background(), Request{Text: "hello", Target: "en", Mode: ModeDict})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if _, err := Drain(ch, nil); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if !llm.called {
		t.Fatal("llm was not called on a hard miss")
	}
	if llm.gotReq.Mode != ModeTranslate {
		t.Errorf("llm req.Mode = %v, want ModeTranslate", llm.gotReq.Mode)
	}
	if llm.gotReq.Preset != PresetDictionary {
		t.Errorf("llm req.Preset = %q, want %q", llm.gotReq.Preset, PresetDictionary)
	}
	if llm.gotReq.Target != "zh" {
		t.Errorf("llm req.Target = %q, want zh (en word, target en → mirror to zh)", llm.gotReq.Target)
	}
}

func TestSmartTarget(t *testing.T) {
	cases := []struct{ text, target, want string }{
		{"你好", "en", "en"}, // Chinese input → English
		{"你好", "", "en"},
		{"hello", "en", "zh"}, // English input, English target → mirror to Chinese
		{"hello", "", "zh"},
		{"hello", "auto", "zh"},
		{"hello", "fr", "fr"}, // an explicit non-English target is honored
	}
	for _, tc := range cases {
		if got := smartTarget(tc.text, tc.target); got != tc.want {
			t.Errorf("smartTarget(%q, %q) = %q, want %q", tc.text, tc.target, got, tc.want)
		}
	}
}
