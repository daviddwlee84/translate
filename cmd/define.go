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

func newDefineCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "define <word>",
		Short: "Look up a word in the dictionary (exact → fuzzy)",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runDefine,
	}
}

func runDefine(cmd *cobra.Command, args []string) error {
	cfg, _, err := config.Load()
	if err != nil {
		return err
	}
	if !cfg.Dict.Enabled {
		return fmt.Errorf("dictionary is disabled in %s", config.Path())
	}
	de := dictFromConfig(cfg)
	word := strings.Join(args, " ")

	ch, err := de.Translate(cmd.Context(), engine.Request{Text: word, Mode: engine.ModeDict})
	if err != nil {
		return err
	}
	res, err := engine.Drain(ch, nil)
	if err != nil {
		return err
	}

	if flagJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	fmt.Print(renderDict(res))
	return nil
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
