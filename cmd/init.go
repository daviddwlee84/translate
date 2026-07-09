package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"

	"translate/internal/config"
	"translate/internal/lang"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Interactive configuration wizard (probes available providers)",
		RunE:  runInit,
	}
}

func runInit(cmd *cobra.Command, _ []string) error {
	cfg, _, err := config.Load()
	if err != nil {
		return err
	}

	// Probe providers so the form can show what's reachable.
	copilotUp := probeProvider(cfg, "copilot")
	ollamaUp := probeProvider(cfg, "ollama")
	openrouterKey := os.Getenv("OPENROUTER_API_KEY") != ""

	engineChoice := cfg.General.Engine
	target := cfg.General.DefaultTarget
	live := cfg.General.LiveTranslate

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Default engine").
				Description("Auto tries each provider in order and falls back.").
				Options(
					huh.NewOption("Auto fallback chain (recommended)", "auto"),
					huh.NewOption(probeLabel("copilot-proxy", copilotUp), "copilot"),
					huh.NewOption(probeLabel("Ollama (offline)", ollamaUp), "ollama"),
					huh.NewOption("Google (free, translate only)", "google"),
				).
				Value(&engineChoice),

			huh.NewInput().
				Title("Default target language").
				Description("Name or code; typos are resolved (e.g. chinees → zh).").
				Value(&target).
				Validate(validateLang),

			huh.NewConfirm().
				Title("Live translate?").
				Description("Auto-translate ~400ms after you stop typing.").
				Value(&live),
		),
	)
	if err := form.Run(); err != nil {
		return err
	}

	if m, _ := lang.Resolve(target); m.Code != "" {
		target = m.Code
	}
	cfg.General.Engine = engineChoice
	cfg.General.DefaultTarget = target
	cfg.General.LiveTranslate = live
	// Refresh copilot model ids to the verified-working recommendations (repairs
	// configs written before a model id changed / was un-served by the proxy).
	if p := cfg.ProviderByName("copilot"); p != nil {
		p.Model, p.ModelFast, p.ModelMax = config.ModelDefault, config.ModelFast, config.ModelMax
	}
	if err := config.Save(cfg); err != nil {
		return err
	}

	fmt.Printf("Saved %s\n  engine=%s  target=%s  live=%v\n", config.Path(), engineChoice, target, live)
	if engineChoice == "copilot" && !copilotUp {
		fmt.Fprintln(os.Stderr, "note: copilot-proxy is not reachable — start it with `copilot-proxy start`.")
	}
	if engineChoice == "ollama" && !ollamaUp {
		fmt.Fprintln(os.Stderr, "note: Ollama is not reachable — start it and `ollama pull llama3.2:3b`.")
	}
	if openrouterKey {
		fmt.Fprintln(os.Stderr, "note: OPENROUTER_API_KEY detected — the openrouter provider is available.")
	}
	return nil
}

func validateLang(s string) error {
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("a target language is required")
	}
	if m, _ := lang.Resolve(s); m.Code == "" {
		return fmt.Errorf("unknown language %q", s)
	}
	return nil
}

func probeLabel(name string, up bool) string {
	if up {
		return name + "  ● up"
	}
	return name + "  ○ down"
}

func probeProvider(cfg *config.Config, name string) bool {
	p := cfg.ProviderByName(name)
	if p == nil {
		return false
	}
	return probeURL(strings.TrimRight(p.BaseURL, "/")+"/models", 1500*time.Millisecond)
}

func probeURL(url string, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
