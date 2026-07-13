package engine

import "testing"

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
