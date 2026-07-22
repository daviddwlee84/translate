package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/daviddwlee84/translate/internal/engine"
)

func TestNextPairModeCycle(t *testing.T) {
	got := []pairMode{}
	m := pairAuto
	for i := 0; i < 4; i++ {
		got = append(got, m)
		m = nextPairMode(m)
	}
	want := []pairMode{pairAuto, pairAway, pairHome, pairOff}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("cycle[%d] = %v, want %v (full: %v)", i, got[i], want[i], got)
		}
	}
	if m != pairAuto {
		t.Fatalf("cycle should return to pairAuto after pairOff, got %v", m)
	}
}

// pairTargetFor is the routing core: which language each ^g direction resolves to,
// and whether the model is asked to auto-detect (only pairAuto) vs a plain
// directed translate (forced modes).
func TestPairTargetFor(t *testing.T) {
	base := Model{target: "zh-TW", pairWith: "en"}
	cases := []struct {
		name       string
		mode       pairMode
		text       string
		wantTarget string
		wantPair   bool
		wantAuto   bool
	}{
		{"auto/english-in", pairAuto, "hello world", "zh-TW", true, true}, // Latin → home (zh)
		{"auto/chinese-in", pairAuto, "你好世界", "en", true, true},           // CJK → away (en)
		{"force-away", pairAway, "anything 隨便", "en", true, false},        // pinned → en, no flip
		{"force-home", pairHome, "anything 隨便", "zh-TW", true, false},     // pinned → zh-TW, no flip
		{"off", pairOff, "你好", "zh-TW", false, false},                     // not a pair
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := base
			m.pairMode = c.mode
			gotT, gotPair, gotAuto := m.pairTargetFor(c.text, engine.ModeTranslate)
			if gotT != c.wantTarget || gotPair != c.wantPair || gotAuto != c.wantAuto {
				t.Fatalf("pairTargetFor(%q) = (%q,%v,%v); want (%q,%v,%v)",
					c.text, gotT, gotPair, gotAuto, c.wantTarget, c.wantPair, c.wantAuto)
			}
		})
	}

	// A non-translate mode (e.g. dictionary) never pairs.
	m := base
	m.pairMode = pairAuto
	if _, pairOn, _ := m.pairTargetFor("hi", engine.ModeDict); pairOn {
		t.Error("dictionary mode must not activate pairing")
	}
}

// Drive the real ^g keypress through Update to prove the handler wiring cycles
// auto → →away → →home → off → auto.
func TestTogglePairKeyCycles(t *testing.T) {
	m := New(context.Background(), Params{
		Engines: []NamedEngine{{Name: "auto", Mode: engine.ModeTranslate}},
		Target:  "zh-TW", PairWith: "en", Pair: true,
	})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = tm.(Model)
	if m.pairMode != pairAuto {
		t.Fatalf("initial pairMode = %v, want pairAuto", m.pairMode)
	}
	ctrlG := tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl}
	if ctrlG.String() != "ctrl+g" {
		t.Fatalf("synthesized key is %q, want ctrl+g", ctrlG.String())
	}
	for i, w := range []pairMode{pairAway, pairHome, pairOff, pairAuto} {
		tm, _ = m.Update(ctrlG)
		m = tm.(Model)
		if m.pairMode != w {
			t.Fatalf("after ^g #%d: pairMode = %v, want %v", i+1, m.pairMode, w)
		}
	}
}

func TestFooterPairLabel(t *testing.T) {
	m := New(context.Background(), Params{
		Engines: []NamedEngine{{Name: "auto", Mode: engine.ModeTranslate}},
		Target:  "zh-TW", PairWith: "en", Pair: true,
	})
	want := map[pairMode]string{
		pairAuto: "pair zh-TW⇄en",
		pairAway: "pair →en",
		pairHome: "pair →zh-TW",
	}
	for mode, sub := range want {
		m.pairMode = mode
		got := ansiRe.ReplaceAllString(m.footerContent(false), "")
		if !strings.Contains(got, sub) {
			t.Errorf("mode %v: footer missing %q\n got: %s", mode, sub, got)
		}
	}
}
