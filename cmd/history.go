package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"translate/internal/config"
	"translate/internal/store"
)

func newHistoryCmd() *cobra.Command {
	var limit int
	var tsv bool

	c := &cobra.Command{
		Use:   "history",
		Short: "Show recent translation history",
		RunE: func(cmd *cobra.Command, _ []string) error {
			recs, err := loadHistory(cmd, func(st store.Store) ([]store.Record, error) {
				return st.Recent(cmd.Context(), limit)
			})
			if err != nil {
				return err
			}
			return printRecords(recs, tsv)
		},
	}
	c.Flags().IntVar(&limit, "limit", 20, "max entries")
	c.Flags().BoolVar(&tsv, "tsv", false, "tab-separated output (for scripting / tv)")

	search := &cobra.Command{
		Use:   "search <query>",
		Short: "Fuzzy-search translation history",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := strings.Join(args, " ")
			recs, err := loadHistory(cmd, func(st store.Store) ([]store.Record, error) {
				return st.Search(cmd.Context(), q, limit)
			})
			if err != nil {
				return err
			}
			return printRecords(recs, tsv)
		},
	}
	search.Flags().IntVar(&limit, "limit", 20, "max entries")
	search.Flags().BoolVar(&tsv, "tsv", false, "tab-separated output")
	c.AddCommand(search)
	return c
}

// loadHistory opens the store and runs a query, handling the disabled case.
func loadHistory(cmd *cobra.Command, query func(store.Store) ([]store.Record, error)) ([]store.Record, error) {
	cfg, _, err := config.Load()
	if err != nil {
		return nil, err
	}
	st := openStore(cfg)
	if st == nil {
		return nil, fmt.Errorf("history is disabled in %s", config.Path())
	}
	defer st.Close()
	return query(st)
}

func printRecords(recs []store.Record, tsv bool) error {
	if flagJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(recs)
	}
	if tsv {
		for _, r := range recs {
			fmt.Printf("%s\t%s\t%s>%s\t%s\t%s\t%s\n",
				r.ID, r.TS.Format("2006-01-02T15:04:05"),
				r.SourceLang, r.TargetLang, r.Engine,
				escapeTSV(r.Input), escapeTSV(r.Output))
		}
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	for _, r := range recs {
		fmt.Fprintf(w, "%s→%s\t%s\t→ %s\t(%s)\n",
			r.SourceLang, r.TargetLang, oneline(r.Input, 40), oneline(r.Output, 40), r.Engine)
	}
	return w.Flush()
}

func escapeTSV(s string) string {
	r := strings.NewReplacer("\t", " ", "\n", " ", "\r", " ")
	return r.Replace(s)
}

func oneline(s string, n int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n-1]) + "…"
}
