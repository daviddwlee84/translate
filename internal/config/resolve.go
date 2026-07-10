package config

import (
	"os"
	"strings"
)

// Overrides carries explicit CLI-flag values (empty string == flag not set).
type Overrides struct {
	Source       string
	Target       string
	Engine       string
	Provider     string
	Model        string
	Tier         string
	Preset       string
	Instructions string
	Pair         bool // --pair forces pair mode on
	PairWith     string
}

// Mode identifies which front-end an invocation resolves for, selecting the
// [cli] or [tui] overlay (if present).
type Mode int

const (
	ModeCLI Mode = iota // one-shot: arguments or piped stdin
	ModeTUI             // interactive Bubble Tea front-end
)

// Resolved is the effective, merged view handed to the engine layer and TUI.
type Resolved struct {
	Source       string // raw language string (fuzzy-resolved by the lang package)
	Target       string
	Engine       string // "auto" or a provider name
	Provider     *Provider
	Model        string
	Tier         string
	Preset       string
	Instructions string
	Pair         bool
	PairWith     string
	Stream       bool
	Color        string

	// LiveTranslate/DebounceMs are TUI-only knobs, resolved here (through the
	// per-mode overlay) so the front-end never reads c.General directly.
	LiveTranslate bool
	DebounceMs    int

	Cfg *Config
}

// Precedence per setting is flag > env (TRANSLATE_*) > [cli]/[tui] overlay > config
// > built-in default. (Env is kept above config so a one-off `TRANSLATE_TARGET=fr`
// works without editing config.toml; flags still win over both.)
func envVal(name string) string { return strings.TrimSpace(os.Getenv(name)) }

// overlayFor returns the per-front-end overlay for a mode, or nil when none is set.
func overlayFor(c *Config, mode Mode) *Overlay {
	switch mode {
	case ModeCLI:
		return c.CLI
	case ModeTUI:
		return c.TUI
	}
	return nil
}

// applyOverlay copies each set (non-nil) overlay field onto g. Model is not a
// General field; Resolve reads it from the overlay directly.
func applyOverlay(g *General, ov *Overlay) {
	if ov == nil {
		return
	}
	if ov.Engine != nil {
		g.Engine = *ov.Engine
	}
	if ov.Tier != nil {
		g.Tier = *ov.Tier
	}
	if ov.Preset != nil {
		g.Preset = *ov.Preset
	}
	if ov.Instructions != nil {
		g.Instructions = *ov.Instructions
	}
	if ov.DefaultTarget != nil {
		g.DefaultTarget = *ov.DefaultTarget
	}
	if ov.DefaultSource != nil {
		g.DefaultSource = *ov.DefaultSource
	}
	if ov.Pair != nil {
		g.Pair = *ov.Pair
	}
	if ov.PairWith != nil {
		g.PairWith = *ov.PairWith
	}
	if ov.Stream != nil {
		g.Stream = *ov.Stream
	}
	if ov.LiveTranslate != nil {
		g.LiveTranslate = *ov.LiveTranslate
	}
	if ov.DebounceMs != nil {
		g.DebounceMs = *ov.DebounceMs
	}
}

// Resolve merges flag overrides, environment variables, the per-mode overlay, and
// config into the effective settings for one invocation.
func (c *Config) Resolve(o Overrides, mode Mode) Resolved {
	// g is the effective [general] after the [cli]/[tui] overlay (a value copy, so
	// the on-disk config is untouched).
	g := c.General
	ov := overlayFor(c, mode)
	applyOverlay(&g, ov)

	pick := func(flag, envName, cfgVal string) string {
		if flag != "" {
			return flag
		}
		if v := envVal(envName); v != "" {
			return v
		}
		return cfgVal
	}

	r := Resolved{
		Cfg:           c,
		Source:        pick(o.Source, "TRANSLATE_SOURCE", g.DefaultSource),
		Target:        pick(o.Target, "TRANSLATE_TARGET", g.DefaultTarget),
		Engine:        pick(o.Engine, "TRANSLATE_ENGINE", g.Engine),
		Tier:          pick(o.Tier, "TRANSLATE_TIER", g.Tier),
		Preset:        pick(o.Preset, "TRANSLATE_PRESET", g.Preset),
		Instructions:  pick(o.Instructions, "TRANSLATE_INSTRUCTIONS", g.Instructions),
		Pair:          o.Pair || g.Pair || envVal("TRANSLATE_PAIR") != "",
		PairWith:      pick(o.PairWith, "TRANSLATE_PAIR_WITH", g.PairWith),
		Color:         g.Color,
		Stream:        g.Stream,
		LiveTranslate: g.LiveTranslate,
		DebounceMs:    g.DebounceMs,
	}

	// Resolve which provider backs an LLM request.
	provName := pick(o.Provider, "TRANSLATE_PROVIDER", "")
	if provName == "" {
		if c.ProviderByName(r.Engine) != nil {
			provName = r.Engine // engine was itself a provider name (e.g. "copilot")
		} else {
			provName = c.firstLLMProvider()
		}
	}
	r.Provider = c.ProviderByName(provName)

	// Resolve the model: explicit override, else the overlay's model, else the
	// provider's tier model.
	cfgModel := ""
	if ov != nil && ov.Model != nil {
		cfgModel = *ov.Model
	}
	r.Model = pick(o.Model, "TRANSLATE_MODEL", cfgModel)
	if r.Model == "" && r.Provider != nil {
		r.Model = r.Provider.ModelForTier(r.Tier)
	}
	return r
}

// firstLLMProvider returns the first provider named in chain.Order that exists,
// else the first configured provider, else "".
func (c *Config) firstLLMProvider() string {
	for _, name := range c.Chain.Order {
		if c.ProviderByName(name) != nil {
			return name
		}
	}
	if len(c.Providers) > 0 {
		return c.Providers[0].Name
	}
	return ""
}
