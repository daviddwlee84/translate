package cmd

import (
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/daviddwlee84/translate/internal/config"
	"github.com/daviddwlee84/translate/internal/lang"
)

// Cobra gives us the `completion` subcommand for free, but flag *values* are a
// different matter: without the registrations below, `translate --to <TAB>`
// falls back to filename completion, which is never useful for a language code.
// The data these draw on is the same data `translate lang list --json` and
// `translate models --json` already expose.
//
// Debug a completion with the hidden driver command, e.g.
//   translate __complete --to ""
// which prints the candidate list followed by a :<directive> line.

// staticCompletions are the closed value sets. Each entry is "value\tdescription";
// cobra splits on the tab and shells that support it render the description.
var staticCompletions = map[string][]string{
	"tier": {
		"default\tthe provider's standard model",
		"fast\tsnappier, cheaper model",
		"max\tthe provider's strongest model",
	},
	"preset": {
		"concise\tterse output, no commentary",
		"contextual\tnatural phrasing with light context",
		"dictionary\tgloss plus example sentences",
	},
	"learn-mode": {
		"auto\tpick teach or correct from the input",
		"teach\tnative to foreign, with a gloss",
		"correct\tgrammar-correct foreign input and explain",
		"explain\tanswer what a term means in the context asked",
	},
	"bilingual-mode": {
		"doc\tcontext-aware, one LLM call",
		"blocks\tper-block translation",
	},
}

// registerCompletions wires value completion onto the root command's flags.
// Errors are only possible when a flag name does not exist, which
// TestFlagCompletionsAreRegistered catches.
func registerCompletions(root *cobra.Command) {
	// Positional args are free text to translate, so offering filenames is noise.
	root.ValidArgsFunction = cobra.NoFileCompletions

	for name, values := range staticCompletions {
		vals := values // capture per iteration
		_ = root.RegisterFlagCompletionFunc(name,
			func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
				return vals, cobra.ShellCompDirectiveNoFileComp
			})
	}

	// "auto" is meaningful as a source but not as a target.
	for _, name := range []string{"from", "speak-lang"} {
		_ = root.RegisterFlagCompletionFunc(name, completeLangWithAuto)
	}
	for _, name := range []string{"to", "pair-with"} {
		_ = root.RegisterFlagCompletionFunc(name, completeLang)
	}

	_ = root.RegisterFlagCompletionFunc("provider", completeProvider)
	_ = root.RegisterFlagCompletionFunc("engine", completeEngine)
	_ = root.RegisterFlagCompletionFunc("model", completeModel)
}

// completedFlagNames is the set of flags registerCompletions covers, used by the
// test to assert none of them silently fell back to filename completion.
func completedFlagNames() []string {
	names := []string{"from", "speak-lang", "to", "pair-with", "provider", "engine", "model"}
	for name := range staticCompletions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func langValues(includeAuto bool) []string {
	langs := lang.List()
	out := make([]string, 0, len(langs)+1)
	if includeAuto {
		out = append(out, "auto\tdetect the source language")
	}
	for _, l := range langs {
		out = append(out, l.Code+"\t"+l.Name)
	}
	return out
}

func completeLang(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return langValues(false), cobra.ShellCompDirectiveNoFileComp
}

func completeLangWithAuto(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return langValues(true), cobra.ShellCompDirectiveNoFileComp
}

func providerNames(cfg *config.Config) []string {
	out := make([]string, 0, len(cfg.Providers))
	for i := range cfg.Providers {
		if n := cfg.Providers[i].Name; n != "" {
			out = append(out, n)
		}
	}
	return out
}

func completeProvider(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	cfg := config.LoadForRead()
	out := make([]string, 0, len(cfg.Providers))
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if p.Name == "" {
			continue
		}
		out = append(out, p.Name+"\t"+p.Type)
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// completeEngine offers the built-in engine names plus every configured
// provider. "dict" is deliberately absent: it is only valid inside chain.order,
// not as an --engine value (see appcore.BuildEngine).
func completeEngine(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	out := []string{
		"smartauto\tdictionary for words, LLM for phrases",
		"auto\tfallback chain over chain.order",
		"llm\tthe resolved provider",
		"google\tfree web translation API",
	}
	for _, name := range providerNames(config.LoadForRead()) {
		out = append(out, name+"\tconfigured provider")
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// completeModel offers the models declared by the provider named in --provider,
// or by every provider when that flag is unset.
func completeModel(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	cfg := config.LoadForRead()

	providers := make([]*config.Provider, 0, len(cfg.Providers))
	if want, _ := cmd.Flags().GetString("provider"); want != "" {
		if p := cfg.ProviderByName(want); p != nil {
			providers = append(providers, p)
		}
	} else {
		for i := range cfg.Providers {
			providers = append(providers, &cfg.Providers[i])
		}
	}

	seen := map[string]bool{}
	out := []string{}
	for _, p := range providers {
		for _, tier := range []string{"default", "fast", "max"} {
			m := p.ModelForTier(tier)
			if m == "" {
				continue
			}
			key := p.Name + "/" + m
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, m+"\t"+p.Name+" "+tier)
		}
	}
	sort.Strings(out)
	return out, cobra.ShellCompDirectiveNoFileComp
}

// completionValue strips the "\tdescription" half of a candidate.
func completionValue(candidate string) string {
	if i := strings.IndexByte(candidate, '\t'); i >= 0 {
		return candidate[:i]
	}
	return candidate
}
