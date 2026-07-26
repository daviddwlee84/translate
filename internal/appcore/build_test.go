package appcore

import (
	"testing"

	"github.com/daviddwlee84/translate/internal/config"
	"github.com/daviddwlee84/translate/internal/engine"
)

// An explicit --model must reach the resolved provider even on the default
// (auto/smartauto) engine. It used to be dropped there — the flag only took
// effect when the caller also named a provider with --engine — which made a
// model picker in any front-end silently do nothing.
func TestBuildAutoChainHonorsExplicitModel(t *testing.T) {
	c := config.Default()
	res := c.Resolve(config.Overrides{Engine: "auto", Model: "claude-opus-4-8"}, config.ModeCLI)
	if res.Provider == nil {
		t.Skip("default config has no provider")
	}
	eng, err := BuildAutoChain(res)
	if err != nil {
		t.Fatalf("BuildAutoChain: %v", err)
	}
	if !usesModel(eng, res.Provider.Name, "claude-opus-4-8") {
		t.Fatalf("resolved provider %q did not get the explicit model", res.Provider.Name)
	}
	// Every other provider keeps its own tier model: one chain spans several
	// backends, and forcing a Claude id onto Ollama would just fail.
	for i := range c.Providers {
		p := &c.Providers[i]
		if p.Name == res.Provider.Name {
			continue
		}
		if usesModel(eng, p.Name, "claude-opus-4-8") {
			t.Fatalf("provider %q was given another provider's model", p.Name)
		}
	}
}

func TestBuildAutoChainWithoutModelUsesTier(t *testing.T) {
	c := config.Default()
	res := c.Resolve(config.Overrides{Engine: "auto", Tier: "fast"}, config.ModeCLI)
	if res.Provider == nil {
		t.Skip("default config has no provider")
	}
	eng, err := BuildAutoChain(res)
	if err != nil {
		t.Fatalf("BuildAutoChain: %v", err)
	}
	want := res.Provider.ModelForTier("fast")
	if want == "" {
		t.Skip("provider declares no fast model")
	}
	if !usesModel(eng, res.Provider.Name, want) {
		t.Fatalf("want the tier model %q for %q", want, res.Provider.Name)
	}
}

// usesModel reports whether the engine (or any member of a chain) is the named
// provider running the given model.
func usesModel(e engine.Engine, provider, model string) bool {
	for _, m := range flatten(e) {
		if llm, ok := m.(*engine.LLMEngine); ok &&
			llm.Name() == provider && llm.Model() == model {
			return true
		}
	}
	return false
}

func flatten(e engine.Engine) []engine.Engine {
	if ch, ok := e.(*engine.Chain); ok {
		var out []engine.Engine
		for _, m := range ch.Engines() {
			out = append(out, flatten(m)...)
		}
		return out
	}
	return []engine.Engine{e}
}
