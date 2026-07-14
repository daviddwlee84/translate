package config

import "testing"

// TestResolveLearnImpliesPair: enabling learn (via flag) forces pair mode on, since
// learn reuses pair's home/away direction routing.
func TestResolveLearnImpliesPair(t *testing.T) {
	c := Default()
	c.General.Pair = false
	got := c.Resolve(Overrides{Learn: true}, ModeCLI)
	if !got.Learn {
		t.Error("Learn should resolve on when the flag is set")
	}
	if !got.Pair {
		t.Error("Learn should imply Pair")
	}
}

// TestResolveLearnOverlay: a per-front-end [tui] overlay can default learn on for the
// TUI while the CLI stays off, mirroring the other overlay knobs.
func TestResolveLearnOverlay(t *testing.T) {
	c := Default()
	c.TUI = &Overlay{Learn: boolptr(true)}
	if tui := c.Resolve(Overrides{}, ModeTUI); !tui.Learn || !tui.Pair {
		t.Errorf("TUI overlay learn=true should resolve Learn && Pair, got learn=%v pair=%v", tui.Learn, tui.Pair)
	}
	if cli := c.Resolve(Overrides{}, ModeCLI); cli.Learn {
		t.Error("CLI should not inherit the [tui] learn overlay")
	}
}
