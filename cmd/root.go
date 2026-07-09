// Package cmd wires the cobra command tree. The root command dispatches between
// the one-shot CLI (arguments or piped stdin) and the interactive TUI (a TTY
// with no arguments); every path drives the shared engine layer.
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	tea "charm.land/bubbletea/v2"

	"translate/internal/config"
	"translate/internal/engine"
	"translate/internal/lang"
	"translate/internal/state"
	"translate/internal/store"
	"translate/internal/tui"
	"translate/internal/xdgpath"
)

var (
	flagTo        string
	flagFrom      string
	flagModel     string
	flagProvider  string
	flagEngine    string
	flagTier      string
	flagJSON      bool
	flagNoHistory bool
)

// NewRootCmd builds the root command and its subcommands.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "translate [text...]",
		Short: "Fast terminal translation (CLI + TUI)",
		Long: "translate is a fast CLI/TUI translator.\n\n" +
			"  translate \"hola mundo\" --to en    one-shot\n" +
			"  echo hola | translate --to en       pipe\n" +
			"  translate                           interactive TUI",
		SilenceUsage:  true,
		SilenceErrors: true,
		// ArbitraryArgs lets bare text fall through to translation while exact
		// subcommands (config, lang, …) still route. Without it, cobra rejects
		// `translate "hola"` as an unknown command once subcommands exist.
		Args: cobra.ArbitraryArgs,
		RunE: runRoot,
	}
	f := root.PersistentFlags()
	f.StringVarP(&flagTo, "to", "t", "", "target language (fuzzy; e.g. en, chinese)")
	f.StringVarP(&flagFrom, "from", "f", "", "source language (\"auto\" to detect)")
	f.StringVar(&flagModel, "model", "", "LLM model id override")
	f.StringVar(&flagProvider, "provider", "", "provider name (e.g. copilot, ollama)")
	f.StringVar(&flagEngine, "engine", "", "engine: auto|<provider>|google|dict")
	f.StringVar(&flagTier, "tier", "", "model tier: default|fast|max")
	f.BoolVar(&flagJSON, "json", false, "emit the full result as JSON")
	f.BoolVar(&flagNoHistory, "no-history", false, "do not record this translation in history")

	root.SuggestionsMinimumDistance = 2
	root.AddCommand(newConfigCmd(), newLangCmd(), newDefineCmd(), newHistoryCmd(), newInitCmd())
	return root
}

// Execute runs the root command with the given (cancellable) context.
func Execute(ctx context.Context) error {
	return NewRootCmd().ExecuteContext(ctx)
}

// overrides collects the explicitly-set flag values.
func overrides() config.Overrides {
	return config.Overrides{
		Source:   flagFrom,
		Target:   flagTo,
		Engine:   flagEngine,
		Provider: flagProvider,
		Model:    flagModel,
		Tier:     flagTier,
	}
}

func runRoot(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	cfg, created, err := config.Load()
	if err != nil {
		return err
	}
	if created {
		fmt.Fprintf(os.Stderr, "translate: wrote default config to %s (run `translate init` to customize)\n", config.Path())
	}

	res := cfg.Resolve(overrides())
	applyLastPair(cfg, &res) // remember_last_pair: seed source/target from state
	src, tgt := resolvePair(res.Source, res.Target)

	if res.Provider == nil && res.Engine != "auto" {
		return fmt.Errorf("no provider configured; check %s", config.Path())
	}
	eng, err := buildEngine(res)
	if err != nil {
		return err
	}

	st := openStore(cfg)
	if st != nil {
		defer st.Close()
	}

	switch {
	case len(args) > 0:
		text := strings.Join(args, " ")
		r, err := oneShot(ctx, eng, text, src, tgt, res.Stream)
		if err != nil {
			return err
		}
		recordAndRemember(ctx, st, cfg, r, text, src, tgt)
		return nil

	case !term.IsTerminal(int(os.Stdin.Fd())):
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		text := strings.TrimSpace(string(b))
		if text == "" {
			return fmt.Errorf("no input on stdin")
		}
		r, err := oneShot(ctx, eng, text, src, tgt, res.Stream)
		if err != nil {
			return err
		}
		recordAndRemember(ctx, st, cfg, r, text, src, tgt)
		return nil

	default:
		return runTUI(ctx, eng, res, st, src, tgt)
	}
}

// applyLastPair overrides the resolved source/target with the persisted last
// pair when remember_last_pair is on and the user did not pass flags/env.
func applyLastPair(cfg *config.Config, res *config.Resolved) {
	if !cfg.General.RememberLastPair {
		return
	}
	stt, err := state.Load()
	if err != nil || stt == nil {
		return
	}
	if flagFrom == "" && os.Getenv("TRANSLATE_SOURCE") == "" && stt.Source != "" {
		res.Source = stt.Source
	}
	if flagTo == "" && os.Getenv("TRANSLATE_TARGET") == "" && stt.Target != "" {
		res.Target = stt.Target
	}
}

// openStore opens the history store, or returns nil when history is disabled or
// suppressed with --no-history.
func openStore(cfg *config.Config) store.Store {
	if !cfg.History.Enabled || flagNoHistory {
		return nil
	}
	path := cfg.History.Path
	if path == "" {
		path = xdgpath.HistoryFile()
	}
	st, err := store.OpenJSONL(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "translate: history disabled (%v)\n", err)
		return nil
	}
	return st
}

// recordAndRemember writes the translation to history and persists the last pair.
func recordAndRemember(ctx context.Context, st store.Store, cfg *config.Config, res *engine.TranslateResult, input, source, target string) {
	if st != nil {
		_, _ = st.Add(ctx, toRecord(res, input, source, target))
	}
	if cfg.General.RememberLastPair {
		saveLastPair(source, target, res.Engine)
	}
}

// toRecord builds a history Record from a translation result.
func toRecord(res *engine.TranslateResult, input, source, target string) store.Record {
	src := source
	if src == "auto" && res.DetectedSource != "" {
		src = res.DetectedSource
	}
	return store.Record{
		SourceLang:   src,
		TargetLang:   target,
		Engine:       res.Engine,
		Model:        res.Model,
		Input:        input,
		Output:       res.Translation,
		Alternatives: res.Alternatives,
		Notes:        res.Notes,
	}
}

// saveLastPair persists the source/target pair and source mode.
func saveLastPair(source, target, engineName string) {
	mode := "fixed"
	if source == "auto" {
		mode = "auto"
	}
	_ = state.Save(&state.State{
		Source:     source,
		Target:     target,
		SourceMode: mode,
		Engine:     engineName,
	})
}

// runTUI launches the interactive Bubble Tea front-end and persists the last
// pair on exit.
func runTUI(ctx context.Context, eng engine.Engine, res config.Resolved, st store.Store, source, target string) error {
	// A model source for the ^p model picker: the resolved LLM provider (if any).
	var modelSrc engine.ModelLister
	if res.Provider != nil {
		modelSrc = llmFromProvider(res.Provider, res.Model)
	}
	p := tui.Params{
		Engines:     buildEngineSet(res, eng),
		ModelSource: modelSrc,
		Store:       st,
		Source:      source,
		Target:      target,
		Model:       res.Model,
		Live:        res.Cfg.General.LiveTranslate,
		DebounceMs:  res.Cfg.General.DebounceMs,
	}
	final, err := tea.NewProgram(tui.New(ctx, p), tea.WithContext(ctx)).Run()
	if err != nil {
		return err
	}
	if res.Cfg.General.RememberLastPair {
		if fm, ok := final.(tui.Model); ok {
			s, t := fm.Pair()
			saveLastPair(s, t, "")
		}
	}
	return nil
}

// resolvePair fuzzy-resolves the source and target languages, printing an
// "(interpreted X as Y)" note on stderr when a fuzzy correction was applied.
func resolvePair(rawSource, rawTarget string) (source, target string) {
	sm, _ := lang.Resolve(rawSource)
	tm, _ := lang.Resolve(rawTarget)
	if !sm.Exact && sm.Score > 0 && !strings.EqualFold(rawSource, sm.Code) {
		fmt.Fprintf(os.Stderr, "translate: interpreted source %q as %s (%s)\n", rawSource, sm.Code, sm.Name)
	}
	if !tm.Exact && tm.Score > 0 && !strings.EqualFold(rawTarget, tm.Code) {
		fmt.Fprintf(os.Stderr, "translate: interpreted target %q as %s (%s)\n", rawTarget, tm.Code, tm.Name)
	}
	return sm.Code, tm.Code
}

// oneShot translates text once and prints the result, returning it for history.
//
// Tokens stream live to stdout only when stdout is a TTY (so `translate x | pbcopy`
// stays clean) and --json was not requested. Piped output is the plain translation
// with no ANSI; --json emits the full structured result.
func oneShot(ctx context.Context, eng engine.Engine, text, source, target string, streamPref bool) (*engine.TranslateResult, error) {
	stdoutTTY := term.IsTerminal(int(os.Stdout.Fd()))
	stream := streamPref && stdoutTTY && !flagJSON

	req := engine.Request{
		Text:   text,
		Source: source,
		Target: target,
		Mode:   engine.ModeTranslate,
		Stream: stream,
	}
	ch, err := eng.Translate(ctx, req)
	if err != nil {
		return nil, err
	}

	var onTok func(string)
	if stream {
		onTok = func(t string) { fmt.Print(t) }
	}
	res, err := engine.Drain(ch, onTok)
	if err != nil {
		if stream {
			fmt.Println() // terminate any partial line before the error surfaces
		}
		return nil, err
	}

	// Never downgrade silently: if the auto-chain fell back, say which engine
	// failed and why, and how to switch.
	for _, w := range res.Warnings {
		fmt.Fprintf(os.Stderr, "translate: warning: %s\n", w)
	}
	if len(res.Warnings) > 0 {
		fmt.Fprintf(os.Stderr, "translate: used %q (switch with --engine, or check the model/provider)\n", res.Engine)
	}

	switch {
	case flagJSON:
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			return nil, err
		}
	case stream:
		fmt.Println() // newline after the streamed tokens
	default:
		fmt.Println(res.Translation)
	}
	return res, nil
}
