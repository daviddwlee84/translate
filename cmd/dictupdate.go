package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"translate/internal/config"
	"translate/internal/engine"
)

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
