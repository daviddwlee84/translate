// Package appcore holds the transport-agnostic core shared by every front-end:
// the engine builders (constructed from resolved config) and a Service that wraps
// a warm engine + history store behind Translate/Define/History methods. The
// one-shot CLI, the HTTP server (translate serve), and the MCP server (translate
// mcp) all drive this same core and diverge only at their transport edge.
//
// It imports only config/engine/store/lang/debug — never tui or cmd — so both cmd
// and the server packages can import it without an import cycle.
package appcore

import (
	"fmt"
	"time"

	"github.com/daviddwlee84/translate/internal/config"
	"github.com/daviddwlee84/translate/internal/engine"
)

// BuildEngine constructs the Engine for an invocation from resolved settings.
// engine="auto" builds a fallback Chain over the providers named in chain.order;
// engine="smartauto" builds the smart default (dictionary for words, LLM for
// phrases, bidirectional); a named provider builds a single LLM engine.
func BuildEngine(res config.Resolved) (engine.Engine, error) {
	cfg := res.Cfg

	switch res.Engine {
	case "auto":
		return BuildAutoChain(res)
	case "smartauto":
		// Needs an LLM provider for the word-miss fallback and phrase translation;
		// without one, behave like the plain auto chain (google/dict).
		if res.Provider == nil {
			return BuildAutoChain(res)
		}
		return SmartAutoFromConfig(res), nil
	case "google":
		return GoogleFromConfig(cfg), nil
	case "llm":
		// "llm" means "the resolved provider" (already chosen by Resolve).
		if res.Provider != nil {
			return LLMFromProvider(res.Provider, res.Model), nil
		}
		return nil, fmt.Errorf("unknown engine/provider %q (check %s)", res.Engine, config.Path())
	default:
		// A named provider (e.g. "copilot", "ollama").
		if res.Provider != nil {
			return LLMFromProvider(res.Provider, res.Model), nil
		}
		return nil, fmt.Errorf("unknown engine/provider %q (check %s)", res.Engine, config.Path())
	}
}

// BuildAutoChain builds the "auto" fallback Chain over the providers/google named
// in chain.order. (google/dict engines otherwise join only via smart-auto or the
// TUI ^e cycle.)
func BuildAutoChain(res config.Resolved) (engine.Engine, error) {
	cfg := res.Cfg
	var engines []engine.Engine
	for _, name := range cfg.Chain.Order {
		switch {
		case cfg.ProviderByName(name) != nil:
			p := cfg.ProviderByName(name)
			engines = append(engines, LLMFromProvider(p, p.ModelForTier(res.Tier)))
		case name == "google" && cfg.Google.Enabled:
			engines = append(engines, GoogleFromConfig(cfg))
		}
		// "dict" is wired in via smart-auto / the TUI engine cycle.
	}
	if len(engines) == 0 {
		return nil, fmt.Errorf("no engines available in chain.order (check %s)", config.Path())
	}
	if len(engines) == 1 {
		return engines[0], nil
	}
	return engine.NewChain(engines, 5*time.Second), nil
}

// LLMFromProvider builds an LLM engine for a provider using the given model id.
func LLMFromProvider(p *config.Provider, model string) *engine.LLMEngine {
	if model == "" {
		model = p.Model
	}
	return engine.NewLLM(engine.LLMConfig{
		Name:      p.Name,
		BaseURL:   p.BaseURL,
		Model:     model,
		APIKeyEnv: p.APIKeyEnv,
	})
}

// GoogleFromConfig builds the free Google engine from config.
func GoogleFromConfig(cfg *config.Config) *engine.GoogleEngine {
	return engine.NewGoogle(engine.GoogleConfig{
		Endpoint:  cfg.Google.Endpoint,
		ExtraDT:   cfg.Google.ExtraDT,
		UserAgent: cfg.Google.UserAgent,
		Timeout:   time.Duration(cfg.Google.TimeoutMs) * time.Millisecond,
	})
}

// DictFromConfig builds the dictionary engine from config: the offline bilingual
// engine (source=local, default) or the dictionaryapi.dev engine (source=api).
func DictFromConfig(cfg *config.Config) engine.Engine {
	if cfg.Dict.Source == "api" {
		return engine.NewDict(engine.DictConfig{
			Endpoint: cfg.Dict.Endpoint,
			Lang:     cfg.Dict.Lang,
			Fuzzy:    cfg.Dict.Fuzzy,
			Wordlist: cfg.Dict.Wordlist,
		})
	}
	var apiFb *engine.DictEngine
	if cfg.Dict.APIFallback {
		apiFb = engine.NewDict(engine.DictConfig{
			Endpoint: cfg.Dict.Endpoint,
			Lang:     cfg.Dict.Lang,
			Fuzzy:    cfg.Dict.Fuzzy,
			Wordlist: cfg.Dict.Wordlist,
		})
	}
	return engine.NewLocalDict(engine.LocalDictConfig{
		Dir:          cfg.Dict.Dir,
		CedictURL:    cfg.Dict.CedictURL,
		EcdictURL:    cfg.Dict.EcdictURL,
		AutoDownload: cfg.Dict.AutoDownload,
		Fuzzy:        cfg.Dict.Fuzzy,
		APIFallback:  apiFb,
	})
}

// SmartDictFromConfig builds the smart-dict engine: the plain dictionary plus an
// LLM fallback (resolved provider/model) for misses and too-weak fuzzy matches.
// The caller must ensure res.Provider != nil.
func SmartDictFromConfig(res config.Resolved) engine.Engine {
	cfg := res.Cfg
	return engine.NewSmartDict(DictFromConfig(cfg), LLMFromProvider(res.Provider, res.Model), engine.SmartDictConfig{
		CloseDistance: cfg.SmartDict.CloseDistance,
		Preset:        cfg.SmartDict.Preset,
	})
}

// SmartAutoFromConfig builds the smart-auto default: a single word/term is a
// dictionary lookup (smart-dict, with an LLM fallback), and a phrase is an LLM
// translation via the auto chain (so phrases keep provider→google resilience).
// The caller must ensure res.Provider != nil.
func SmartAutoFromConfig(res config.Resolved) engine.Engine {
	llm, err := BuildAutoChain(res)
	if err != nil {
		// No chain engines (unlikely once a provider exists): fall back to the bare
		// resolved provider so phrases can still translate.
		llm = LLMFromProvider(res.Provider, res.Model)
	}
	return engine.NewSmartAuto(SmartDictFromConfig(res), llm)
}

// LearnEngineFromConfig builds the engine for learning mode: a bare LLM engine
// (the resolved provider/model), bypassing smart-auto/dictionary routing so a
// single word still gets the structured tutor treatment. Caller ensures Provider != nil.
func LearnEngineFromConfig(res config.Resolved) engine.Engine {
	return LLMFromProvider(res.Provider, res.Model)
}

// DefineEngine picks the dictionary engine for a definition lookup: the plain
// offline dictionary, or smart-dict (with an LLM fallback) when a provider is
// available and not overridden. plain forces plain; smart forces smart. It is the
// single source of truth shared by `translate define` and the Service.
func DefineEngine(res config.Resolved, plain, smart bool) engine.Engine {
	cfg := res.Cfg
	if plain || res.Provider == nil {
		return DictFromConfig(cfg)
	}
	if smart || cfg.SmartDict.DefineDefault {
		return SmartDictFromConfig(res)
	}
	return DictFromConfig(cfg)
}
