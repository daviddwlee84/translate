package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/huh/v2"
	"github.com/daviddwlee84/translate/internal/config"
	"github.com/daviddwlee84/translate/internal/engine"
)

func promptDictInstall(ctx context.Context, targets []string) (bool, error) {
	download := true
	form := huh.NewForm(huh.NewGroup(huh.NewConfirm().
		Title("Download missing offline dictionaries now?").
		Description(fmt.Sprintf("Missing: %s. Up to ~67 MB; building the English dictionary may take a minute. Your configuration is already saved.", strings.Join(targets, ", "))).
		Affirmative("Download now").Negative("Later").Value(&download))).WithOutput(os.Stderr)
	err := form.RunWithContext(ctx)
	return download, err
}

// Called only after configuration has been saved. A skipped or interrupted
// download cannot undo setup, and a retry never needs another run of the wizard.
func setupMissingDictionaries(ctx context.Context, cfg *config.Config, out io.Writer, confirm func(context.Context, []string) (bool, error)) error {
	if !cfg.Dict.Enabled || cfg.Dict.Source == "api" {
		return nil
	}
	status := engine.InspectLocalDict(ctx, cfg.Dict.Dir)
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("configuration saved; dictionary setup incomplete — run `translate dict update all`: %w", err)
	}
	var missing []string
	if status.CedictSource.State == "missing" && status.CedictIndex.State == "missing" {
		missing = append(missing, "cedict")
	} else if status.CedictSource.State == "ready" && status.CedictIndex.State != "ready" {
		fmt.Fprintln(out, "translate: CC-CEDICT is installed; run `translate dict reindex` for fast Chinese search.")
	} else if status.CedictSource.State != "ready" && status.CedictIndex.State != "ready" {
		bad := status.CedictSource
		if bad.State == "missing" {
			bad = status.CedictIndex
		}
		fmt.Fprintf(out, "translate: CC-CEDICT is unusable (%s: %v) — run `translate dict update cedict` to repair it.\n", bad.Path, bad.Err)
	}
	if status.Ecdict.State == "missing" {
		missing = append(missing, "ecdict")
	} else if status.Ecdict.State == "unusable" {
		fmt.Fprintf(out, "translate: ECDICT is unusable (%s: %v) — run `translate dict update ecdict` to repair it.\n", status.Ecdict.Path, status.Ecdict.Err)
	}
	if len(missing) == 0 {
		return nil
	}
	target := strings.Join(missing, " ")
	if len(missing) == 2 {
		target = "all"
	}
	retry := "translate dict update " + target
	download, err := confirm(ctx, missing)
	if err != nil {
		return fmt.Errorf("configuration saved; dictionary setup incomplete — run `%s`: %w", retry, err)
	}
	if !download {
		fmt.Fprintf(out, "translate: configuration saved; install offline dictionaries later with `%s`.\n", retry)
		return nil
	}
	for i, name := range missing {
		if err := updateDictionaries(ctx, cfg, name, out); err != nil {
			retryTarget := name
			if len(missing[i:]) > 1 {
				retryTarget = "all"
			}
			return fmt.Errorf("configuration saved; dictionary setup incomplete — retry with `translate dict update %s`: %w", retryTarget, err)
		}
	}
	fmt.Fprintln(out, "translate: offline dictionaries installed.")
	return nil
}
