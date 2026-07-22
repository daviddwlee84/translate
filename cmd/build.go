package cmd

import (
	"github.com/daviddwlee84/translate/internal/appcore"
	"github.com/daviddwlee84/translate/internal/config"
	"github.com/daviddwlee84/translate/internal/engine"
	"github.com/daviddwlee84/translate/internal/tui"
)

// buildEngineSet builds the list of engines the TUI can cycle through with ^e:
// the resolved primary (translate), plus Google (fast, keyless), the dictionary
// (word lookup), and — when an LLM provider is available — the smart dictionary
// (dictionary with an LLM fallback), so the user can switch on the fly.
//
// The engine constructors themselves live in internal/appcore (shared with the CLI
// one-shot path and the serve/mcp servers); this wrapper stays in cmd because it
// depends on internal/tui, which appcore must not import.
func buildEngineSet(res config.Resolved, primary engine.Engine) []tui.NamedEngine {
	cfg := res.Cfg
	primaryName := primary.Name() // "auto", "smart-auto", "copilot", "google", …
	if primaryName == "" {
		primaryName = "auto"
	}
	set := []tui.NamedEngine{{Name: primaryName, Engine: primary, Mode: engine.ModeTranslate}}
	if primaryName != "google" && cfg.Google.Enabled {
		set = append(set, tui.NamedEngine{Name: "google", Engine: appcore.GoogleFromConfig(cfg), Mode: engine.ModeTranslate})
	}
	if cfg.Dict.Enabled {
		set = append(set, tui.NamedEngine{Name: "dictionary", Engine: appcore.DictFromConfig(cfg), Mode: engine.ModeDict})
		if res.Provider != nil {
			set = append(set, tui.NamedEngine{Name: "smart-dict", Engine: appcore.SmartDictFromConfig(res), Mode: engine.ModeDict})
		}
	}
	return set
}
