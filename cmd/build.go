package cmd

import (
	"fmt"
	"time"

	"translate/internal/config"
	"translate/internal/engine"
)

// buildEngine constructs the Engine for an invocation from resolved settings.
// engine="auto" builds a fallback Chain over the providers named in chain.order;
// a named provider builds a single LLM engine. (google/dict engines join the
// chain in later slices.)
func buildEngine(res config.Resolved) (engine.Engine, error) {
	cfg := res.Cfg

	if res.Engine != "auto" {
		switch res.Engine {
		case "google":
			return googleFromConfig(cfg), nil
		case "llm":
			// "llm" means "the resolved provider" (already chosen by Resolve).
		}
		if res.Provider != nil {
			return llmFromProvider(res.Provider, res.Model), nil
		}
		return nil, fmt.Errorf("unknown engine/provider %q (check %s)", res.Engine, config.Path())
	}

	var engines []engine.Engine
	for _, name := range cfg.Chain.Order {
		switch {
		case cfg.ProviderByName(name) != nil:
			p := cfg.ProviderByName(name)
			engines = append(engines, llmFromProvider(p, p.ModelForTier(res.Tier)))
		case name == "google" && cfg.Google.Enabled:
			engines = append(engines, googleFromConfig(cfg))
		}
		// "dict" is wired in a later slice.
	}
	if len(engines) == 0 {
		return nil, fmt.Errorf("no engines available in chain.order (check %s)", config.Path())
	}
	if len(engines) == 1 {
		return engines[0], nil
	}
	return engine.NewChain(engines, 5*time.Second), nil
}

// llmFromProvider builds an LLM engine for a provider using the given model id.
func llmFromProvider(p *config.Provider, model string) *engine.LLMEngine {
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

// googleFromConfig builds the free Google engine from config.
func googleFromConfig(cfg *config.Config) *engine.GoogleEngine {
	return engine.NewGoogle(engine.GoogleConfig{
		Endpoint:  cfg.Google.Endpoint,
		ExtraDT:   cfg.Google.ExtraDT,
		UserAgent: cfg.Google.UserAgent,
		Timeout:   time.Duration(cfg.Google.TimeoutMs) * time.Millisecond,
	})
}

// dictFromConfig builds the dictionary engine from config.
func dictFromConfig(cfg *config.Config) *engine.DictEngine {
	return engine.NewDict(engine.DictConfig{
		Endpoint: cfg.Dict.Endpoint,
		Lang:     cfg.Dict.Lang,
		Fuzzy:    cfg.Dict.Fuzzy,
		Wordlist: cfg.Dict.Wordlist,
	})
}
