package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/daviddwlee84/translate/internal/config"
	"github.com/daviddwlee84/translate/internal/engine"
)

var (
	flagDefinePlain bool
	flagDefineSmart bool
)

func newDefineCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "define <word>",
		Short: "Look up a word in the dictionary (exact → fuzzy → LLM)",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runDefine,
	}
	c.Flags().BoolVar(&flagDefinePlain, "plain", false, "force the offline dictionary (no LLM fallback)")
	c.Flags().BoolVar(&flagDefineSmart, "smart", false, "force smart-dict (LLM fallback on a miss)")
	return c
}

func runDefine(cmd *cobra.Command, args []string) error {
	cfg, _, err := config.Load()
	if err != nil {
		return err
	}
	if !cfg.Dict.Enabled {
		return fmt.Errorf("dictionary is disabled in %s", config.Path())
	}
	res := cfg.Resolve(overrides(), config.ModeCLI)
	de := defineEngine(res)
	word := strings.Join(args, " ")

	ch, err := de.Translate(cmd.Context(), engine.Request{Text: word, Mode: engine.ModeDict, Stream: false})
	if err != nil {
		return err
	}
	res2, err := engine.Drain(ch, nil)
	if err != nil {
		return err
	}

	if flagJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(res2)
	}
	// Surface an LLM-fallback downgrade the same way the CLI translate path does.
	for _, w := range res2.Warnings {
		fmt.Fprintf(os.Stderr, "translate: warning: %s\n", w)
	}
	fmt.Print(renderDict(res2))
	return nil
}

// defineEngine picks the dictionary engine for `translate define`: the plain
// offline dictionary, or smart-dict (with an LLM fallback) when a provider is
// available and not overridden. --plain forces plain; --smart forces smart.
func defineEngine(res config.Resolved) engine.Engine {
	cfg := res.Cfg
	if flagDefinePlain || res.Provider == nil {
		return dictFromConfig(cfg)
	}
	if flagDefineSmart || cfg.SmartDict.DefineDefault {
		return smartDictFromConfig(res)
	}
	return dictFromConfig(cfg)
}

// renderDict formats a dictionary result as plain text (no ANSI, pipe-safe).
func renderDict(res *engine.TranslateResult) string {
	d := res.Dictionary
	if d == nil {
		if len(res.Suggestions) > 0 {
			return "no exact match — did you mean: " + strings.Join(res.Suggestions, ", ") + "\n"
		}
		return res.Translation + "\n"
	}
	var b strings.Builder
	head := d.Word
	if d.Phonetic != "" {
		head += "  " + d.Phonetic
	}
	b.WriteString(head + "\n")
	for _, m := range d.Meanings {
		b.WriteString("  " + m.PartOfSpeech + "\n")
		for i, def := range m.Definitions {
			if i >= 4 { // cap definitions per part of speech
				break
			}
			b.WriteString("    • " + def.Text + "\n")
			if def.Example != "" {
				b.WriteString("      \"" + def.Example + "\"\n")
			}
		}
	}
	return b.String()
}
