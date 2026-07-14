package tts

import "testing"

func TestSayVoice(t *testing.T) {
	tests := []struct {
		code string
		want string
	}{
		{"en", "Samantha"},
		{"zh", "Tingting"},
		{"zh-CN", "Tingting"},
		{"zh-TW", "Meijia"},
		{"zh-HK", "Sinji"},
		{"ja", "Kyoko"},
		{"ZH-tw", "Meijia"}, // case-insensitive
		{"xx", ""},          // unknown → default (empty)
	}
	for _, tc := range tests {
		if got := sayVoice(nil, tc.code); got != tc.want {
			t.Errorf("sayVoice(%q) = %q, want %q", tc.code, got, tc.want)
		}
	}
}

func TestSayVoiceOverride(t *testing.T) {
	ov := map[string]string{"en": "Alex", "zh": "Custom"}
	if got := sayVoice(ov, "en"); got != "Alex" {
		t.Errorf("override en = %q, want Alex", got)
	}
	// exact miss on the override, base "zh" hit wins over the built-in.
	if got := sayVoice(ov, "zh-CN"); got != "Custom" {
		t.Errorf("override base zh for zh-CN = %q, want Custom", got)
	}
}

func TestEspeakVoice(t *testing.T) {
	tests := []struct{ code, want string }{
		{"zh", "cmn"},
		{"zh-HK", "yue"},
		{"ja", "ja"},
		{"es", "es"}, // base fallback
		{"fr", "fr"},
	}
	for _, tc := range tests {
		if got := espeakVoice(nil, tc.code); got != tc.want {
			t.Errorf("espeakVoice(%q) = %q, want %q", tc.code, got, tc.want)
		}
	}
}

func TestGoogleTL(t *testing.T) {
	tests := []struct{ code, want string }{
		{"zh", "zh-CN"},
		{"zh-CN", "zh-CN"},
		{"zh-TW", "zh-TW"},
		{"zh-HK", "zh-TW"},
		{"en", "en"},
		{"fr", "fr"},
		{"EN", "en"},
	}
	for _, tc := range tests {
		if got := googleTL(tc.code); got != tc.want {
			t.Errorf("googleTL(%q) = %q, want %q", tc.code, got, tc.want)
		}
	}
}

func TestChunkText(t *testing.T) {
	if got := chunkText("short", 200); len(got) != 1 || got[0] != "short" {
		t.Errorf("short chunk = %v", got)
	}
	long := ""
	for i := 0; i < 50; i++ {
		long += "word "
	}
	got := chunkText(long, 20)
	if len(got) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(got))
	}
	for _, c := range got {
		if len([]rune(c)) > 20 {
			t.Errorf("chunk exceeds limit: %q (%d)", c, len([]rune(c)))
		}
	}
}
