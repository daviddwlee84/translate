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
	"translate/internal/engine"
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
	preset := cfg.General.Preset
	if preset == "" {
		preset = engine.PresetContextual
	}
	instructions := cfg.General.Instructions

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

			huh.NewSelect[string]().
				Title("Default target language").
				Description("Type to filter; auto-detect is always the source.").
				Options(targetLangOptions()...).
				Height(12).
				Filtering(true).
				Value(&target),

			huh.NewSelect[string]().
				Title("Translation style").
				Description("How the LLM formats translations.").
				Options(
					huh.NewOption("contextual — translations across common senses", engine.PresetContextual),
					huh.NewOption("concise — terse direct translation", engine.PresetConcise),
					huh.NewOption("dictionary — translation + example sentences", engine.PresetDictionary),
				).
				Value(&preset),

			huh.NewText().
				Title("Custom instructions (optional)").
				Description("Extra guidance for the LLM, e.g. domain focus: quantitative finance + computer science.").
				Value(&instructions),

			huh.NewConfirm().
				Title("Live translate?").
				Description("Auto-translate as you type. Off by default to avoid spamming the API.").
				Value(&live),
		),
	)
	if err := form.Run(); err != nil {
		return err
	}

	cfg.General.Engine = engineChoice
	cfg.General.DefaultTarget = target
	cfg.General.LiveTranslate = live
	cfg.General.Preset = preset
	cfg.General.Instructions = strings.TrimSpace(instructions)
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

// targetLangOptions builds the language dropdown options (name + code).
func targetLangOptions() []huh.Option[string] {
	ls := lang.List()
	opts := make([]huh.Option[string], 0, len(ls))
	for _, l := range ls {
		opts = append(opts, huh.NewOption(fmt.Sprintf("%s (%s)", l.Name, l.Code), l.Code))
	}
	return opts
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
