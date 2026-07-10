package config

import "testing"

func strptr(s string) *string { return &s }

// A [cli] / [tui] overlay overrides [general] for its front-end only; an unset
// overlay field inherits [general].
func TestResolvePerModeOverlay(t *testing.T) {
	c := Default()
	c.General.Preset = "contextual"
	c.General.Tier = "fast"
	c.CLI = &Overlay{Preset: strptr("concise"), Tier: strptr("max")}
	c.TUI = &Overlay{Preset: strptr("dictionary")} // no tier → inherits "fast"

	cli := c.Resolve(Overrides{}, ModeCLI)
	if cli.Preset != "concise" {
		t.Errorf("CLI preset = %q, want concise", cli.Preset)
	}
	if cli.Tier != "max" {
		t.Errorf("CLI tier = %q, want max", cli.Tier)
	}

	tui := c.Resolve(Overrides{}, ModeTUI)
	if tui.Preset != "dictionary" {
		t.Errorf("TUI preset = %q, want dictionary", tui.Preset)
	}
	if tui.Tier != "fast" {
		t.Errorf("TUI tier = %q, want fast (inherited from [general])", tui.Tier)
	}
}

// With no overlay tables, both modes see plain [general].
func TestResolveNoOverlayInheritsGeneral(t *testing.T) {
	c := Default()
	c.General.Preset = "contextual"
	if got := c.Resolve(Overrides{}, ModeCLI); got.Preset != "contextual" {
		t.Errorf("CLI preset = %q, want contextual", got.Preset)
	}
	if got := c.Resolve(Overrides{}, ModeTUI); got.Preset != "contextual" {
		t.Errorf("TUI preset = %q, want contextual", got.Preset)
	}
}

// Precedence: flag > env > overlay > [general].
func TestResolveFlagBeatsOverlay(t *testing.T) {
	c := Default()
	c.General.Preset = "contextual"
	c.CLI = &Overlay{Preset: strptr("concise")}
	got := c.Resolve(Overrides{Preset: "dictionary"}, ModeCLI)
	if got.Preset != "dictionary" {
		t.Errorf("preset = %q, want dictionary (flag beats overlay)", got.Preset)
	}
}

func TestResolveEnvBeatsOverlay(t *testing.T) {
	c := Default()
	c.General.Preset = "contextual"
	c.CLI = &Overlay{Preset: strptr("concise")}
	t.Setenv("TRANSLATE_PRESET", "dictionary")
	if got := c.Resolve(Overrides{}, ModeCLI); got.Preset != "dictionary" {
		t.Errorf("preset = %q, want dictionary (env beats overlay)", got.Preset)
	}
}

// The overlay's model becomes the config-level fallback for the model pick.
func TestResolveOverlayModel(t *testing.T) {
	c := Default()
	c.TUI = &Overlay{Model: strptr("claude-opus-4-8")}
	if got := c.Resolve(Overrides{}, ModeTUI); got.Model != "claude-opus-4-8" {
		t.Errorf("TUI model = %q, want claude-opus-4-8", got.Model)
	}
	// CLI has no overlay model → falls through to the provider's tier model.
	cli := c.Resolve(Overrides{}, ModeCLI)
	if cli.Model == "claude-opus-4-8" {
		t.Errorf("CLI model = %q, should not inherit the TUI overlay model", cli.Model)
	}
}

// LiveTranslate/DebounceMs are resolved through the overlay (TUI-only knobs).
func TestResolveOverlayLiveTranslate(t *testing.T) {
	c := Default()
	c.General.LiveTranslate = false
	c.General.DebounceMs = 700
	c.TUI = &Overlay{LiveTranslate: boolptr(true), DebounceMs: intptr(300)}
	tui := c.Resolve(Overrides{}, ModeTUI)
	if !tui.LiveTranslate || tui.DebounceMs != 300 {
		t.Errorf("TUI live=%v debounce=%d, want true/300", tui.LiveTranslate, tui.DebounceMs)
	}
	cli := c.Resolve(Overrides{}, ModeCLI)
	if cli.LiveTranslate || cli.DebounceMs != 700 {
		t.Errorf("CLI live=%v debounce=%d, want false/700 (inherited)", cli.LiveTranslate, cli.DebounceMs)
	}
}

func boolptr(b bool) *bool { return &b }
func intptr(i int) *int    { return &i }
