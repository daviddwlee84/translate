// Package config defines translate's on-disk configuration (config.toml) and the
// resolution of effective settings (flags > config > env > defaults).
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/daviddwlee84/translate/internal/xdgpath"
)

// Config is the top-level config.toml schema.
type Config struct {
	// Version is the app version (git tag) that last wrote this file, and Schema is
	// the config schema generation (see SchemaVersion). Both are stamped by Save;
	// Version is informational, Schema drives the "your config is outdated" reminder.
	// Declared first so they serialize above the [general] table.
	Version string `toml:"version,omitempty"`
	Schema  int    `toml:"schema"`

	General   General    `toml:"general"`
	CLI       *Overlay   `toml:"cli,omitempty"` // per-front-end overrides on [general]
	TUI       *Overlay   `toml:"tui,omitempty"` // (nil unless the user adds the table)
	Chain     Chain      `toml:"chain"`
	Providers []Provider `toml:"provider"` // array-of-tables: [[provider]]
	Google    Google     `toml:"google"`
	Dict      Dict       `toml:"dict"`
	SmartDict SmartDict  `toml:"smartdict"`
	TTS       TTS        `toml:"tts"`
	History   History    `toml:"history"`
	Server    Server     `toml:"server"`
}

// SchemaVersion is the current config schema generation. Bump it whenever a new
// field or default is added that `translate init` would materialize, so an older
// on-disk config (schema < this) can be detected and the user reminded to re-init.
// It is NOT the app version (which is the git tag; see cmd/version.go). History:
//
//	1 — smart-auto default, pair-mode home anchoring, debug, TTS/learn fields.
//	2 — [server] table (translate serve: HTTP API).
const SchemaVersion = 2

// Generator is the app version string (git tag) stamped into config.version on
// Save. The cmd layer sets it at startup; it stays "" for library/test callers,
// in which case the version key is simply omitted.
var Generator string

// Overlay is a partial [general] applied for one front-end ([cli] or [tui]). Every
// field is a pointer so a nil means "inherit from [general]"; only keys the user
// actually wrote override. Precedence: flag > env > [cli]/[tui] > [general] > default.
type Overlay struct {
	Engine        *string `toml:"engine,omitempty"`
	Tier          *string `toml:"tier,omitempty"`
	Preset        *string `toml:"preset,omitempty"`
	Instructions  *string `toml:"instructions,omitempty"`
	Model         *string `toml:"model,omitempty"`
	DefaultTarget *string `toml:"default_target,omitempty"`
	DefaultSource *string `toml:"default_source,omitempty"`
	Pair          *bool   `toml:"pair,omitempty"`
	PairWith      *string `toml:"pair_with,omitempty"`
	Learn         *bool   `toml:"learn,omitempty"`
	Stream        *bool   `toml:"stream,omitempty"`
	LiveTranslate *bool   `toml:"live_translate,omitempty"`
	DebounceMs    *int    `toml:"debounce_ms,omitempty"`
	Debug         *bool   `toml:"debug,omitempty"`
}

// General holds behavior settings.
type General struct {
	DefaultTarget     string `toml:"default_target"`
	DefaultSource     string `toml:"default_source"`      // "auto" or a fixed code
	Pair              bool   `toml:"pair"`                // bidirectional: home-language input → pair_with, else → default_target
	PairWith          string `toml:"pair_with,omitempty"` // the "away" language for pair mode
	Learn             bool   `toml:"learn"`               // learning mode: teach (native→foreign) or grammar-correct (foreign→native)
	RememberLastPair  bool   `toml:"remember_last_pair"`
	LiveTranslate     bool   `toml:"live_translate"`
	DebounceMs        int    `toml:"debounce_ms"`
	Engine            string `toml:"engine"`                 // auto | smartauto | llm | google | <provider>
	Tier              string `toml:"tier"`                   // default | fast | max
	Preset            string `toml:"preset"`                 // concise | contextual | dictionary
	Instructions      string `toml:"instructions,omitempty"` // extra system-prompt guidance (domain focus, etc.)
	AlternativesCount int    `toml:"alternatives_count"`
	Stream            bool   `toml:"stream"`
	Color             string `toml:"color"` // auto | always | never
	Debug             bool   `toml:"debug"` // verbose decision logging (also --debug / TRANSLATE_DEBUG)
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

// TTS configures free, cross-platform text-to-speech (pronunciation playback).
// Backends are tried in Order: "native" (macOS say / Linux espeak / Windows SAPI,
// offline) then "google" (translate_tts MP3 + a discovered player).
type TTS struct {
	Enabled       bool              `toml:"enabled"`
	AutoSpeak     bool              `toml:"auto_speak"`        // CLI: speak every result without --speak
	Order         []string          `toml:"order,omitempty"`   // e.g. ["native","google"]
	Foreign       string            `toml:"foreign,omitempty"` // the 副/away language to speak; "" => derive
	PreferForeign bool              `toml:"prefer_foreign"`    // auto-side selection prefers the foreign side
	Rate          int               `toml:"rate,omitempty"`    // native words/min; 0 => backend default
	Voices        map[string]string `toml:"voices,omitempty"`  // lang -> native voice override
	GoogleTTSURL  string            `toml:"google_tts_url,omitempty"`
	UserAgent     string            `toml:"user_agent,omitempty"`
	TimeoutMs     int               `toml:"timeout_ms,omitempty"`
	CacheDir      string            `toml:"cache_dir,omitempty"`
	Player        string            `toml:"player,omitempty"` // forced audio player binary
}

// History configures translation history storage.
type History struct {
	Enabled bool   `toml:"enabled"`
	Backend string `toml:"backend"` // jsonl | sqlite
	Path    string `toml:"path,omitempty"`
}

// Server configures the local HTTP API server (`translate serve`). The bind is
// loopback by default because history is personal data; a non-loopback bind is
// refused unless a token is set. Prefer token_env over an inline token so the
// secret stays out of config.toml.
type Server struct {
	Port     int    `toml:"port"`                // listen port (default 4155)
	Bind     string `toml:"bind"`                // bind address (default 127.0.0.1)
	Token    string `toml:"token,omitempty"`     // inline bearer token (guards /v1/history)
	TokenEnv string `toml:"token_env,omitempty"` // env var holding the token (preferred)
}

// ServerOverrides carries explicit `translate serve` flag values (zero == unset).
type ServerOverrides struct {
	Port  int
	Bind  string
	Token string
}

// ResolvedServer is the effective server config after flags/env are applied.
type ResolvedServer struct {
	Port  int
	Bind  string
	Token string // resolved token (inline or from token_env); "" => no auth
}

// ResolveServer merges flag overrides over the [server] table and resolves the
// bearer token (flag > inline token > token_env), filling in the built-in
// defaults for an empty port/bind (an older config predating [server] reads 0/"").
func (c *Config) ResolveServer(o ServerOverrides) ResolvedServer {
	port := c.Server.Port
	if o.Port != 0 {
		port = o.Port
	}
	if port == 0 {
		port = 4155
	}
	bind := c.Server.Bind
	if o.Bind != "" {
		bind = o.Bind
	}
	if bind == "" {
		bind = "127.0.0.1"
	}
	token := o.Token
	if token == "" {
		token = c.Server.Token
	}
	if token == "" && c.Server.TokenEnv != "" {
		token = strings.TrimSpace(os.Getenv(c.Server.TokenEnv))
	}
	return ResolvedServer{Port: port, Bind: bind, Token: token}
}

// SmartDict configures the smart-dict engine: a dictionary lookup that falls back
// to the LLM when it misses or the fuzzy match is too far off (see engine.SmartDict).
type SmartDict struct {
	// CloseDistance is the English edit-distance at/below which a fuzzy "did you
	// mean" is treated as a likely typo and kept as-is; beyond it (and any hard
	// miss) the lookup falls back to the LLM.
	CloseDistance int    `toml:"close_distance"`
	Preset        string `toml:"preset,omitempty"` // LLM prompt style for the fallback
	DefineDefault bool   `toml:"define_default"`   // `translate define` uses smart-dict when a provider is available
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
		SmartDict: SmartDict{
			CloseDistance: 1,            // distance-1 typos stay "did you mean"; farther → LLM
			Preset:        "dictionary", // gloss + example sentences
			DefineDefault: true,         // `translate define` prefers smart-dict when a provider is up
		},
		TTS: TTS{
			Enabled:       true,
			AutoSpeak:     false, // opt-in per call via --speak / TUI ^s
			Order:         []string{"native", "google"},
			Foreign:       "", // "" => derive the non-Chinese side to speak
			PreferForeign: true,
			GoogleTTSURL:  "https://translate.google.com/translate_tts",
			UserAgent:     "Mozilla/5.0 translate-cli",
			TimeoutMs:     5000,
		},
		History: History{
			Enabled: true,
			Backend: "jsonl",
		},
		Server: Server{
			Port: 4155,
			Bind: "127.0.0.1",
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

// Save writes the config atomically (temp file + rename). It stamps the current
// schema generation and (when known) the writing app version, so a later build
// can detect an outdated config.
func Save(cfg *Config) error {
	if err := xdgpath.EnsureDirs(); err != nil {
		return err
	}
	cfg.Schema = SchemaVersion
	if Generator != "" {
		cfg.Version = Generator
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

// Outdated reports whether the loaded config predates the current schema, i.e.
// it was written by an older build (or before schema tracking existed, in which
// case the key is absent and reads as 0). The caller uses it to nudge a re-init.
func (c *Config) Outdated() bool { return c.Schema < SchemaVersion }

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
