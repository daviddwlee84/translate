package appcore

import (
	"testing"

	"github.com/daviddwlee84/translate/internal/engine"
)

// Recordable is the guard that keeps "did you mean" misses and install hints out
// of history — they used to land there as empty-output rows.
func TestRecordable(t *testing.T) {
	cases := []struct {
		name string
		res  *engine.TranslateResult
		want bool
	}{
		{"nil", nil, false},
		{"normal translation", &engine.TranslateResult{Translation: "貓"}, true},
		{"suggestions only", &engine.TranslateResult{Suggestions: []string{"hello"}, Engine: "dictionary"}, false},
		{"not installed note", &engine.TranslateResult{Notes: "ECDICT not installed", Engine: "dictionary"}, false},
		{"whitespace output", &engine.TranslateResult{Translation: "  \n "}, false},
		{
			"dictionary entry with an empty gloss",
			&engine.TranslateResult{Dictionary: &engine.DictEntry{Word: "cat"}},
			true,
		},
		{"learn result", &engine.TranslateResult{Learn: &engine.LearnResult{}}, true},
		{"truncated stream", &engine.TranslateResult{Translation: "half a sen", Truncated: true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Recordable(tc.res); got != tc.want {
				t.Fatalf("Recordable = %v, want %v", got, tc.want)
			}
		})
	}
}
