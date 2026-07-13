package lang

import "testing"

func TestPairTargetCJKNonCJK(t *testing.T) {
	cases := []struct {
		name             string
		home, away, text string
		want             string
	}{
		// home=zh-TW, away=en (the ideal config): symmetric and reliable.
		{"zh-home/english-in", "zh-TW", "en", "test", "zh-TW"},
		{"zh-home/chinese-in", "zh-TW", "en", "你好", "en"},
		// home=en, away=zh-TW (reversed): must still work — this is the case the old
		// asymmetric router broke on short Latin input.
		{"en-home/english-in", "en", "zh-TW", "test", "zh-TW"},
		{"en-home/chinese-in", "en", "zh-TW", "測試", "en"},
		// A mixed sentence with any Han routes to the non-CJK side.
		{"mixed-han", "zh-TW", "en", "hello 世界", "en"},
		// Degenerate configs collapse to home (a no-op) rather than misbehaving.
		{"same-lang", "en", "en", "test", "en"},
		{"empty-away", "zh-TW", "", "test", "zh-TW"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := PairTarget(c.home, c.away, c.text); got != c.want {
				t.Errorf("PairTarget(%q,%q,%q) = %q, want %q", c.home, c.away, c.text, got, c.want)
			}
		})
	}
}

func TestContainsCJK(t *testing.T) {
	yes := []string{"你好", "テスト", "가", "abc世界"}
	no := []string{"test", "hello world", "café", "123"}
	for _, s := range yes {
		if !containsCJK(s) {
			t.Errorf("containsCJK(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if containsCJK(s) {
			t.Errorf("containsCJK(%q) = true, want false", s)
		}
	}
}
