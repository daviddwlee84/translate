package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/daviddwlee84/translate/internal/config"
)

// modelOut is one selectable model. Only models the config actually declares are
// listed: a provider's /v1/models can advertise ids it then refuses to serve
// (copilot-proxy serves claude-* only via /v1/messages), so probing it would
// offer choices that fail at request time.
type modelOut struct {
	Provider string `json:"provider"`
	Tier     string `json:"tier"` // "fast" | "default" | "max"
	Model    string `json:"model"`
	// Default marks the model this config would use with no --model/--tier.
	Default bool `json:"default,omitempty"`
}

func newModelsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "models",
		Short: "List the models declared by the configured providers",
		Long: "List the models declared by the configured providers, one per tier.\n\n" +
			"Only config-declared models are listed. A provider's /v1/models can\n" +
			"advertise ids it will not actually serve, so probing it would offer\n" +
			"choices that fail at request time.",
		Args: cobra.NoArgs,
		RunE: runModels,
	}
}

func runModels(cmd *cobra.Command, args []string) error {
	cfg, _, err := config.Load()
	if err != nil {
		return err
	}
	res := cfg.Resolve(overrides(), config.ModeCLI)
	defProvider, defModel := "", ""
	if res.Provider != nil {
		defProvider = res.Provider.Name
		defModel = res.Provider.ModelForTier(res.Tier)
	}

	var out []modelOut
	seen := map[string]bool{}
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		// "default" first: ModelForTier falls back to Model when model_fast /
		// model_max are unset, so a provider that declares only `model` would
		// otherwise get that model labelled "fast".
		for _, tier := range []string{"default", "fast", "max"} {
			m := p.ModelForTier(tier)
			if m == "" {
				continue
			}
			key := p.Name + "\x00" + m
			if seen[key] {
				continue // model_fast/model_max fall back to model when unset
			}
			seen[key] = true
			out = append(out, modelOut{
				Provider: p.Name,
				Tier:     tier,
				Model:    m,
				Default:  p.Name == defProvider && m == defModel,
			})
		}
	}

	if flagJSON {
		if out == nil {
			out = []modelOut{}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	for _, m := range out {
		mark := ""
		if m.Default {
			mark = "  (default)"
		}
		fmt.Fprintf(w, "%s\t%s\t%s%s\n", m.Provider, m.Tier, m.Model, mark)
	}
	return w.Flush()
}
