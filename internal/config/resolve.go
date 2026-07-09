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
}

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
	Stream       bool
	Color        string
	Cfg          *Config
}

// Precedence per setting is flag > env (TRANSLATE_*) > config > built-in default.
// (Env is kept above config so a one-off `TRANSLATE_TARGET=fr` works without
// editing config.toml; flags still win over both.)
func envVal(name string) string { return strings.TrimSpace(os.Getenv(name)) }

// Resolve merges flag overrides, environment variables, and config into the
// effective settings for one invocation.
func (c *Config) Resolve(o Overrides) Resolved {
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
		Cfg:          c,
		Source:       pick(o.Source, "TRANSLATE_SOURCE", c.General.DefaultSource),
		Target:       pick(o.Target, "TRANSLATE_TARGET", c.General.DefaultTarget),
		Engine:       pick(o.Engine, "TRANSLATE_ENGINE", c.General.Engine),
		Tier:         pick(o.Tier, "TRANSLATE_TIER", c.General.Tier),
		Preset:       pick(o.Preset, "TRANSLATE_PRESET", c.General.Preset),
		Instructions: pick(o.Instructions, "TRANSLATE_INSTRUCTIONS", c.General.Instructions),
		Color:        c.General.Color,
		Stream:       c.General.Stream,
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

	// Resolve the model: explicit override, else the provider's tier model.
	r.Model = pick(o.Model, "TRANSLATE_MODEL", "")
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
