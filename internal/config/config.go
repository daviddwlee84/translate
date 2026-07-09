// Package config defines translate's on-disk configuration (config.toml) and the
// resolution of effective settings (flags > config > env > defaults).
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"

	"translate/internal/xdgpath"
)

// Config is the top-level config.toml schema.
type Config struct {
	General   General    `toml:"general"`
	Chain     Chain      `toml:"chain"`
	Providers []Provider `toml:"provider"` // array-of-tables: [[provider]]
	Google    Google     `toml:"google"`
	Dict      Dict       `toml:"dict"`
	History   History    `toml:"history"`
}

// General holds behavior settings.
type General struct {
	DefaultTarget     string `toml:"default_target"`
	DefaultSource     string `toml:"default_source"`      // "auto" or a fixed code
	Pair              bool   `toml:"pair"`                // bidirectional: home-language input → pair_with, else → default_target
	PairWith          string `toml:"pair_with,omitempty"` // the "away" language for pair mode
	RememberLastPair  bool   `toml:"remember_last_pair"`
	LiveTranslate     bool   `toml:"live_translate"`
	DebounceMs        int    `toml:"debounce_ms"`
	Engine            string `toml:"engine"`                 // auto | llm | google | dict
	Tier              string `toml:"tier"`                   // default | fast | max
	Preset            string `toml:"preset"`                 // concise | contextual | dictionary
	Instructions      string `toml:"instructions,omitempty"` // extra system-prompt guidance (domain focus, etc.)
	AlternativesCount int    `toml:"alternatives_count"`
	Stream            bool   `toml:"stream"`
	Color             string `toml:"color"` // auto | always | never
}

// Chain is the ordered fallback list of provider/engine names.
type Chain struct {
	Order []string `toml:"order"`
}

// Provider is one OpenAI-compatible LLM backend.
type Provider struct {
	Name      string `toml:"name"`
	Type      string `toml:"type"` // openai | ollama | openrouter | litellm | generic
	BaseURL   string `toml:"base_url"`
	Model     string `toml:"model"`
	ModelFast string `toml:"model_fast,omitempty"`
	ModelMax  string `toml:"model_max,omitempty"`
	APIKeyEnv string `toml:"api_key_env,omitempty"` // "" => no Authorization header
}

// Google configures the free translate_a/single endpoint.
type Google struct {
	Enabled   bool     `toml:"enabled"`
	Endpoint  string   `toml:"endpoint,omitempty"`
	ExtraDT   []string `toml:"extra_dt,omitempty"`
	UserAgent string   `toml:"user_agent,omitempty"`
	TimeoutMs int      `toml:"timeout_ms,omitempty"`
}

// Dict configures the dictionary lookup engine.
type Dict struct {
	Enabled  bool   `toml:"enabled"`
	Source   string `toml:"source"` // local (CC-CEDICT + ECDICT) | api (dictionaryapi.dev)
	Endpoint string `toml:"endpoint,omitempty"`
	Lang     string `toml:"lang,omitempty"`
	Fuzzy    bool   `toml:"fuzzy"`
	Wordlist string `toml:"wordlist,omitempty"` // "" => /usr/share/dict/words if present

	// Local bilingual dictionary (source=local).
	Dir          string `toml:"dir,omitempty"` // "" => <XDG data>/dict
	CedictURL    string `toml:"cedict_url,omitempty"`
	EcdictURL    string `toml:"ecdict_url,omitempty"`
	AutoDownload bool   `toml:"auto_download"` // auto-fetch CC-CEDICT (small) on first zh lookup
	APIFallback  bool   `toml:"api_fallback"`  // fall back to dictionaryapi.dev on an English miss
}

// History configures translation history storage.
type History struct {
	Enabled bool   `toml:"enabled"`
	Backend string `toml:"backend"` // jsonl | sqlite
	Path    string `toml:"path,omitempty"`
}

// Recommended copilot-proxy model ids per tier. Claude models are served via the
// Anthropic Messages API (/v1/messages); the LLM engine routes them there
// automatically (they 400 on /chat/completions).
const (
	ModelDefault = "claude-sonnet-5"  // balanced
	ModelFast    = "claude-haiku-4-5" // snappy — the out-of-box default tier
	ModelMax     = "claude-opus-4-8"  // highest quality
)

// Default returns a config with sensible defaults, written on first run.
func Default() *Config {
	return &Config{
		General: General{
			DefaultTarget:     "en",
			DefaultSource:     "auto",
			Pair:              false,
			PairWith:          "en", // used when pair mode is enabled
			RememberLastPair:  true,
			LiveTranslate:     false, // off by default to avoid spamming LLM/API while typing
			DebounceMs:        700,   // when live is on, wait longer before firing
			Engine:            "auto",
			Tier:              "fast", // haiku by default — snappy for short, quick translations
			Preset:            "contextual",
			AlternativesCount: 3,
			Stream:            true,
			Color:             "auto",
		},
		Chain: Chain{Order: []string{"copilot", "ollama", "google", "dict"}},
		Providers: []Provider{
			{
				Name:      "copilot",
				Type:      "openai",
				BaseURL:   "http://localhost:4141/v1",
				Model:     ModelDefault,
				ModelFast: ModelFast,
				ModelMax:  ModelMax,
			},
			{
				Name:    "ollama",
				Type:    "ollama",
				BaseURL: "http://localhost:11434/v1",
				Model:   "llama3.2:3b",
			},
			{
				Name:      "openrouter",
				Type:      "openrouter",
				BaseURL:   "https://openrouter.ai/api/v1",
				Model:     "anthropic/claude-sonnet-5",
				APIKeyEnv: "OPENROUTER_API_KEY",
			},
		},
		Google: Google{
			Enabled:   true,
			Endpoint:  "https://translate.googleapis.com/translate_a/single",
			ExtraDT:   []string{"bd", "at"},
			UserAgent: "Mozilla/5.0 translate-cli",
			TimeoutMs: 4000,
		},
		Dict: Dict{
			Enabled:      true,
			Source:       "local", // CC-CEDICT (zh→en) + ECDICT (en→zh); run `translate dict update`
			Endpoint:     "https://api.dictionaryapi.dev/api/v2/entries",
			Lang:         "en",
			Fuzzy:        true,
			CedictURL:    "https://www.mdbg.net/chinese/export/cedict/cedict_1_0_ts_utf-8_mdbg.txt.gz",
			EcdictURL:    "https://raw.githubusercontent.com/skywind3000/ECDICT/master/ecdict.csv",
			AutoDownload: false, // explicit `dict update` is the blessed path
			APIFallback:  true,  // English lookups still work before ECDICT is installed
		},
		History: History{
			Enabled: true,
			Backend: "jsonl",
		},
	}
}

// Path returns the config file path, honoring $TRANSLATE_CONFIG if set.
func Path() string {
	if p := os.Getenv("TRANSLATE_CONFIG"); p != "" {
		return p
	}
	return xdgpath.ConfigFile()
}

// Load reads the config file. On first run (file missing) it writes Default() and
// returns it with created=true so the caller can hint the user to run `init`.
func Load() (cfg *Config, created bool, err error) {
	p := Path()
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		cfg = Default()
		if werr := Save(cfg); werr != nil {
			return cfg, false, fmt.Errorf("write default config: %w", werr)
		}
		return cfg, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	cfg = Default() // start from defaults so omitted keys keep sane values
	if err := toml.Unmarshal(b, cfg); err != nil {
		return nil, false, fmt.Errorf("parse %s: %w", p, err)
	}
	return cfg, false, nil
}

// Save writes the config atomically (temp file + rename).
func Save(cfg *Config) error {
	if err := xdgpath.EnsureDirs(); err != nil {
		return err
	}
	b, err := toml.Marshal(cfg)
	if err != nil {
		return err
	}
	p := Path()
	tmp, err := os.CreateTemp(filepath.Dir(p), ".config-*.toml")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after successful rename
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, p)
}

// ProviderByName returns the named provider, or nil if absent.
func (c *Config) ProviderByName(name string) *Provider {
	for i := range c.Providers {
		if c.Providers[i].Name == name {
			return &c.Providers[i]
		}
	}
	return nil
}

// ModelForTier returns the provider's model id for the given tier
// ("default"/"fast"/"max"), falling back to the default model.
func (p *Provider) ModelForTier(tier string) string {
	switch tier {
	case "fast":
		if p.ModelFast != "" {
			return p.ModelFast
		}
	case "max":
		if p.ModelMax != "" {
			return p.ModelMax
		}
	}
	return p.Model
}
