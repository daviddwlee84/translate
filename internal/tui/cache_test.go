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
	paired.pairMode = pairAuto
	paired.pairWith = "en"
	if plain.cacheKeyFor("hello") == paired.cacheKeyFor("hello") {
		t.Error("pair on/off must produce distinct cache keys for the same text")
	}

	// Each forced direction pins a different target but shares m.target, so the
	// mode itself must be in the key or →away and →home would collide.
	away, home := paired, paired
	away.pairMode = pairAway
	home.pairMode = pairHome
	keys := map[cacheKey]bool{
		paired.cacheKeyFor("hello"): true,
		away.cacheKeyFor("hello"):   true,
		home.cacheKeyFor("hello"):   true,
	}
	if len(keys) != 3 {
		t.Error("auto / →away / →home pair modes must each produce a distinct cache key")
	}
}
