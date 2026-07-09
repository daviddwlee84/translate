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
	"translate/internal/tui"
)

var (
	flagTo       string
	flagFrom     string
	flagModel    string
	flagProvider string
	flagEngine   string
	flagTier     string
	flagJSON     bool
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

	root.SuggestionsMinimumDistance = 2
	root.AddCommand(newConfigCmd(), newLangCmd(), newDefineCmd())
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
	src, tgt := resolvePair(res.Source, res.Target)

	if res.Provider == nil && res.Engine != "auto" {
		return fmt.Errorf("no provider configured; check %s", config.Path())
	}
	eng, err := buildEngine(res)
	if err != nil {
		return err
	}

	switch {
	case len(args) > 0:
		return oneShot(ctx, eng, strings.Join(args, " "), src, tgt, res.Stream)

	case !term.IsTerminal(int(os.Stdin.Fd())):
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		text := strings.TrimSpace(string(b))
		if text == "" {
			return fmt.Errorf("no input on stdin")
		}
		return oneShot(ctx, eng, text, src, tgt, res.Stream)

	default:
		return runTUI(ctx, eng, res, src, tgt)
	}
}

// runTUI launches the interactive Bubble Tea front-end.
func runTUI(ctx context.Context, eng engine.Engine, res config.Resolved, source, target string) error {
	providerName := res.Engine
	if res.Engine != "auto" && res.Provider != nil {
		providerName = res.Provider.Name
	}
	p := tui.Params{
		Engine:     eng,
		Source:     source,
		Target:     target,
		Provider:   providerName,
		Model:      res.Model,
		Live:       res.Cfg.General.LiveTranslate,
		DebounceMs: res.Cfg.General.DebounceMs,
	}
	prog := tea.NewProgram(tui.New(ctx, p), tea.WithContext(ctx))
	_, err := prog.Run()
	return err
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

// oneShot translates text once and prints the result.
//
// Tokens stream live to stdout only when stdout is a TTY (so `translate x | pbcopy`
// stays clean) and --json was not requested. Piped output is the plain translation
// with no ANSI; --json emits the full structured result.
func oneShot(ctx context.Context, eng engine.Engine, text, source, target string, streamPref bool) error {
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
		return err
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
		return err
	}

	switch {
	case flagJSON:
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	case stream:
		fmt.Println() // newline after the streamed tokens
	default:
		fmt.Println(res.Translation)
	}
	return nil
}
