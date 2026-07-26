package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/daviddwlee84/translate/internal/lang"
)

// langOut is the JSON shape of a supported language. Front-ends (the Raycast
// extension's language picker) read this instead of hardcoding a subset.
type langOut struct {
	Code    string   `json:"code"`
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
}

// resolveOut is the JSON shape of `lang resolve`.
type resolveOut struct {
	Query        string    `json:"query"`
	Code         string    `json:"code"`
	Name         string    `json:"name"`
	Exact        bool      `json:"exact"`
	Score        float64   `json:"score"`
	Alternatives []langAlt `json:"alternatives,omitempty"`
}

type langAlt struct {
	Code  string  `json:"code"`
	Name  string  `json:"name"`
	Score float64 `json:"score"`
}

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
			if flagJSON {
				out := resolveOut{Query: args[0], Code: m.Code, Name: m.Name, Exact: m.Exact, Score: m.Score}
				for _, a := range alts {
					out.Alternatives = append(out.Alternatives, langAlt{Code: a.Code, Name: a.Name, Score: a.Score})
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}
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
	c.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List the supported target languages",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			langs := lang.List()
			if flagJSON {
				out := make([]langOut, 0, len(langs))
				for _, l := range langs {
					out = append(out, langOut{Code: l.Code, Name: l.Name, Aliases: l.Aliases})
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			for _, l := range langs {
				fmt.Fprintf(w, "%s\t%s\n", l.Code, l.Name)
			}
			return w.Flush()
		},
	})
	return c
}
