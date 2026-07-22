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
		// Routing follows which script DOMINATES, not mere presence:
		// a CJK-majority mixed sentence still routes to the non-CJK side …
		{"mixed-han-majority", "zh-TW", "en", "hello 世界", "en"},
		// … but a few CJK proper nouns inside a long Latin passage must NOT flip it
		// to English (the reported bug: mostly-English text came back unchanged).
		{"mostly-latin-few-han", "zh-TW", "en", "The song by 李榮浩 and 李白 was a genuine hit worldwide", "zh-TW"},
		{"latin-with-cjk-filenames", "zh-TW", "en", "renamed li-ronghao-libai and shexiang-furen; verified 李榮浩/李白 exact match", "zh-TW"},
		// A CJK sentence with an embedded Latin loanword still routes to Latin.
		{"cjk-with-loanword", "zh-TW", "en", "我今天在用 iPhone 打字", "en"},
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

func TestCJKDominant(t *testing.T) {
	dominant := []string{
		"你好世界",            // pure CJK
		"hello 世界",        // 2 Han > 1 Latin word
		"我今天在用 iPhone 打字", // Han majority despite a loanword
		"テストです",           // Japanese kana
	}
	notDominant := []string{
		"test",
		"hello world",
		"",
		"123 + 456",
		"The song by 李榮浩 and 李白 was a genuine hit worldwide", // few Han in Latin prose
		"renamed li-ronghao-libai; verified 李榮浩/李白 exact match",
	}
	for _, s := range dominant {
		if !cjkDominant(s) {
			t.Errorf("cjkDominant(%q) = false, want true", s)
		}
	}
	for _, s := range notDominant {
		if cjkDominant(s) {
			t.Errorf("cjkDominant(%q) = true, want false", s)
		}
	}
}
