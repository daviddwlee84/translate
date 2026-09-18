package engine

import (
	"context"
	"testing"
)

func TestIsLookup(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		// Single words/terms → dictionary lookup.
		{"test", true},
		{"escalations", true},
		{"run-time", true},
		{"don't", true},
		{"你好", true},         // 2 Han runes
		{"  spaced  ", true}, // trims to one token
		// Phrases / non-words → LLM translate.
		{"hello world", false},
		{"This is a sentence.", false},
		{"測試一下下下", false},  // >4 Han runes → treat as a phrase
		{"test123", false}, // digits are not a plain word
		{"", false},
	}
	for _, c := range cases {
		if got := isLookup(c.text); got != c.want {
			t.Errorf("isLookup(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestSmartAutoPreserveFormatRouting(t *testing.T) {
	for _, preserve := range []bool{false, true} {
		name := "ordinary word uses dictionary"
		if preserve {
			name = "forced document uses translation"
		}
		t.Run(name, func(t *testing.T) {
			dict := &fakeEngine{name: "dictionary", res: &TranslateResult{Translation: "字典"}}
			llm := &fakeEngine{name: "llm", res: &TranslateResult{Translation: "翻譯"}}
			eng := NewSmartAuto(dict, llm)
			req := Request{
				Text: "test", Source: "auto", Target: "zh-TW", Mode: ModeTranslate,
				Stream: true, PreserveFormat: preserve,
			}
			ch, err := eng.Translate(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Drain(ch, nil); err != nil {
				t.Fatal(err)
			}
			if llm.called != preserve || dict.called == preserve {
				t.Fatalf("preserve=%v: dictionary called=%v, translation called=%v", preserve, dict.called, llm.called)
			}
			got := llm.gotReq
			if !preserve {
				got = dict.gotReq
				req.Mode = ModeDict
			}
			if got.Mode != req.Mode || got.Stream != req.Stream || got.PreserveFormat != preserve || got.Text != req.Text {
				t.Errorf("routed request = %+v, expected mode=%v and original text/stream/preservation", got, req.Mode)
			}
		})
	}
}
