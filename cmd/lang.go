package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/daviddwlee84/translate/internal/lang"
)

func newLangCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "lang",
		Short: "Language-code utilities",
	}
	c.AddCommand(&cobra.Command{
		Use:   "resolve <query>",
		Short: "Fuzzy-resolve a language name/code (e.g. chinees -> zh)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, alts := lang.Resolve(args[0])
			kind := "fuzzy"
			if m.Exact {
				kind = "exact"
			}
			fmt.Printf("%s -> %s (%s) [%s, score %.2f]\n", args[0], m.Code, m.Name, kind, m.Score)
			for _, a := range alts {
				fmt.Printf("  ~ %s (%s) %.2f\n", a.Code, a.Name, a.Score)
			}
			return nil
		},
	})
	return c
}
