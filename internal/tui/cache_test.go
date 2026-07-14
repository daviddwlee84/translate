package tui

import "testing"

// TestCacheKeyLearnDistinct: a learn result and a plain translation of the same text
// must not collide in the session cache (they hold different content).
func TestCacheKeyLearnDistinct(t *testing.T) {
	base := Model{
		p:      Params{Engines: []NamedEngine{{Name: "auto"}}},
		source: "auto",
		target: "zh-TW",
	}
	plain := base // learn:false
	learn := base
	learn.learn = true

	if plain.cacheKeyFor("hello") == learn.cacheKeyFor("hello") {
		t.Error("learn on/off must produce distinct cache keys for the same text")
	}

	// Pair on/off must also be distinct (fixes a pre-existing latent collision).
	paired := base
	paired.pair = true
	paired.pairWith = "en"
	if plain.cacheKeyFor("hello") == paired.cacheKeyFor("hello") {
		t.Error("pair on/off must produce distinct cache keys for the same text")
	}
}
