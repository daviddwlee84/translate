package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/daviddwlee84/translate/internal/appcore"
	"github.com/daviddwlee84/translate/internal/config"
	"github.com/daviddwlee84/translate/internal/engine"
)

var flagDictSearchLimit int

func newDictCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "dict",
		Short: "Manage the local bilingual dictionary (CC-CEDICT + ECDICT)",
	}
	c.AddCommand(&cobra.Command{
		Use:   "update [cedict|ecdict|all]",
		Short: "Download/build the local dictionaries (~67 MB one-time)",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runDictUpdate,
	})
	c.AddCommand(&cobra.Command{
		Use:   "reindex [cedict]",
		Short: "Rebuild the CC-CEDICT search index from the downloaded file (no network)",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runDictReindex,
	})
	search := &cobra.Command{
		Use:   "search <prefix>",
		Short: "Search dictionary headwords by prefix (ranked, with previews)",
		Long: "Search dictionary headwords by prefix, ranked by frequency, with a\n" +
			"one-line definition preview per candidate. Reads only local data — no\n" +
			"network, no LLM — so it is cheap enough to run on every keystroke.\n\n" +
			"Finding nothing is not an error: the result is an empty candidate list\n" +
			"and exit status 0.",
		Args: cobra.MinimumNArgs(1),
		RunE: runDictSearch,
	}
	search.Flags().IntVar(&flagDictSearchLimit, "limit", 12, "max candidates to return")
	c.AddCommand(search)
	return c
}

func runDictUpdate(cmd *cobra.Command, args []string) error {
	cfg, _, err := config.Load()
	if err != nil {
		return err
	}
	what := "all"
	if len(args) == 1 {
		what = args[0]
	}
	dir := engine.DictDir(cfg.Dict.Dir)
	ctx := cmd.Context()
	prog := func(s string) { fmt.Fprintln(os.Stderr, "  "+s) }

	if what == "cedict" || what == "all" {
		fmt.Fprintf(os.Stderr, "CC-CEDICT (Chinese→English):\n")
		if err := engine.DownloadCedict(ctx, cfg.Dict.CedictURL, engine.CedictPath(dir), prog); err != nil {
			return fmt.Errorf("cedict: %w", err)
		}
		fmt.Fprintf(os.Stderr, "  -> %s\n", engine.CedictPath(dir))
		// The plain file is re-parsed by every process (~1.7 s); the index makes
		// lookups a point query. A build failure is not fatal — the file still works.
		if err := engine.BuildCedictDB(ctx, engine.CedictPath(dir), engine.CedictDBPath(dir), prog); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: index build failed (%v) — run `translate dict reindex` later\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "  -> %s\n", engine.CedictDBPath(dir))
		}
	}
	if what == "ecdict" || what == "all" {
		fmt.Fprintf(os.Stderr, "ECDICT (English→Chinese, this takes a minute):\n")
		if err := engine.BuildEcdictDB(ctx, cfg.Dict.EcdictURL, engine.EcdictDBPath(dir), prog); err != nil {
			return fmt.Errorf("ecdict: %w", err)
		}
		fmt.Fprintf(os.Stderr, "  -> %s\n", engine.EcdictDBPath(dir))
	}
	if what != "cedict" && what != "ecdict" && what != "all" {
		return fmt.Errorf("unknown target %q (use cedict|ecdict|all)", what)
	}
	fmt.Fprintln(os.Stderr, "done. Dictionary mode (^e) is now bilingual zh↔en.")
	return nil
}

// runDictReindex rebuilds the CC-CEDICT index from the already-downloaded file.
// It exists for installs that predate the index, so nobody has to re-download.
func runDictReindex(cmd *cobra.Command, args []string) error {
	cfg, _, err := config.Load()
	if err != nil {
		return err
	}
	if len(args) == 1 && args[0] != "cedict" {
		return fmt.Errorf("unknown target %q (only cedict has a rebuildable index)", args[0])
	}
	dir := engine.DictDir(cfg.Dict.Dir)
	src := engine.CedictPath(dir)
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("CC-CEDICT not downloaded yet — run `translate dict update cedict`")
	}
	prog := func(s string) { fmt.Fprintln(os.Stderr, "  "+s) }
	if err := engine.BuildCedictDB(cmd.Context(), src, engine.CedictDBPath(dir), prog); err != nil {
		return fmt.Errorf("cedict index: %w", err)
	}
	fmt.Fprintf(os.Stderr, "  -> %s\n", engine.CedictDBPath(dir))
	return nil
}

func runDictSearch(cmd *cobra.Command, args []string) error {
	cfg, _, err := config.Load()
	if err != nil {
		return err
	}
	q := strings.Join(args, " ")
	if !cfg.Dict.Enabled {
		return fmt.Errorf("dictionary is disabled in %s", config.Path())
	}
	s, ok := appcore.DictSearcher(cfg)
	if !ok {
		// [dict] source = "api": dictionaryapi.dev has no headword list to scan.
		return printSearch(&engine.SearchResult{
			Query:      q,
			Candidates: []engine.Candidate{},
			Source:     "none",
			Notes:      `headword search needs the offline dictionary ([dict] source = "local")`,
		})
	}
	res, err := s.Search(cmd.Context(), q, flagDictSearchLimit)
	if err != nil {
		return err
	}
	return printSearch(res)
}

func printSearch(res *engine.SearchResult) error {
	if flagJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	if res.Notes != "" {
		fmt.Fprintf(os.Stderr, "translate: %s\n", res.Notes)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	for _, c := range res.Candidates {
		fmt.Fprintf(w, "%s\t%s\n", c.Word, oneline(c.Preview, 72))
	}
	return w.Flush()
}
