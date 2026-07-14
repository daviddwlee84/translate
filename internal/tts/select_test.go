package tts

import (
	"testing"

	"github.com/daviddwlee84/translate/internal/lang"
)

func TestSelect(t *testing.T) {
	tests := []struct {
		name     string
		in       SelectInput
		wantText string
		wantBase string // expected lang.Base of the chosen language
		wantOK   bool
	}{
		{
			name:     "foreign en: zh source, en result → result",
			in:       SelectInput{SourceText: "你好世界", SourceLang: "zh", ResultText: "hello world", ResultLang: "en", Foreign: "en"},
			wantText: "hello world", wantBase: "en", wantOK: true,
		},
		{
			name:     "foreign en: en source, zh result → source",
			in:       SelectInput{SourceText: "hello world", SourceLang: "en", ResultText: "你好世界", ResultLang: "zh-TW", Foreign: "en"},
			wantText: "hello world", wantBase: "en", wantOK: true,
		},
		{
			name:     "derive foreign (non-zh side): zh source, en result → result",
			in:       SelectInput{SourceText: "你好", SourceLang: "zh", ResultText: "hello", ResultLang: "en"},
			wantText: "hello", wantBase: "en", wantOK: true,
		},
		{
			name:     "forced result overrides foreign",
			in:       SelectInput{SourceText: "hello", SourceLang: "en", ResultText: "你好世界", ResultLang: "zh-TW", Foreign: "en", Forced: SideResult},
			wantText: "你好世界", wantBase: "zh", wantOK: true,
		},
		{
			name:     "forced source overrides foreign",
			in:       SelectInput{SourceText: "你好世界", SourceLang: "zh-TW", ResultText: "hello", ResultLang: "en", Foreign: "en", Forced: SideSource},
			wantText: "你好世界", wantBase: "zh", wantOK: true,
		},
		{
			name:     "forced result empty falls through to auto",
			in:       SelectInput{SourceText: "hello", SourceLang: "en", ResultText: "", Foreign: "en", Forced: SideResult},
			wantText: "hello", wantBase: "en", wantOK: true,
		},
		{
			name:   "both empty → not ok",
			in:     SelectInput{Foreign: "en"},
			wantOK: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Select(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (got %+v)", ok, tc.wantOK, got)
			}
			if !ok {
				return
			}
			if got.Text != tc.wantText {
				t.Errorf("Text = %q, want %q", got.Text, tc.wantText)
			}
			if b := lang.Base(got.Lang); b != tc.wantBase {
				t.Errorf("lang base = %q (from %q), want %q", b, got.Lang, tc.wantBase)
			}
		})
	}
}

// TestSelectRegionPreserved verifies the zh-TW region survives (needed to pick
// the Taiwan voice), since offline detection can only recover the base "zh".
func TestSelectRegionPreserved(t *testing.T) {
	got, ok := Select(SelectInput{ResultText: "測試", ResultLang: "zh-TW", Forced: SideResult})
	if !ok {
		t.Fatal("want ok")
	}
	if got.Lang != "zh-tw" {
		t.Errorf("Lang = %q, want zh-tw (region preserved)", got.Lang)
	}
}
