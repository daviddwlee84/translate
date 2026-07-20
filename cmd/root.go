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
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"github.com/daviddwlee84/translate/internal/bitext"
	"github.com/daviddwlee84/translate/internal/config"
	"github.com/daviddwlee84/translate/internal/debug"
	"github.com/daviddwlee84/translate/internal/engine"
	"github.com/daviddwlee84/translate/internal/lang"
	"github.com/daviddwlee84/translate/internal/state"
	"github.com/daviddwlee84/translate/internal/store"
	"github.com/daviddwlee84/translate/internal/tui"
	"github.com/daviddwlee84/translate/internal/xdgpath"
)

var (
	flagTo           string
	flagFrom         string
	flagModel        string
	flagProvider     string
	flagEngine       string
	flagTier         string
	flagPreset       string
	flagInstructions string
	flagPair         bool
	flagPairWith     string
	flagLearn        bool
	flagBilingual    bool
	flagJSON         bool
	flagNoHistory    bool
	flagDebug        bool
	flagSpeak        bool
	flagSpeakLang    string
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
		Version:       buildVersion(),
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
	f.StringVar(&flagPreset, "preset", "", "LLM prompt style: concise|contextual|dictionary")
	f.StringVar(&flagInstructions, "instructions", "", "extra system-prompt guidance (domain focus, etc.)")
	f.BoolVar(&flagPair, "pair", false, "bidirectional mode: home-language input → --pair-with, else → --to")
	f.StringVar(&flagPairWith, "pair-with", "", "the other language for --pair (e.g. en)")
	f.BoolVar(&flagLearn, "learn", false, "learning mode: teach (native→foreign) or grammar-correct (foreign→native)")
	f.BoolVarP(&flagBilingual, "bilingual", "2", false, "bilingual pipe mode: keep original (with color) + translation beneath (stdin only)")
	f.BoolVar(&flagJSON, "json", false, "emit the full result as JSON")
	f.BoolVar(&flagNoHistory, "no-history", false, "do not record this translation in history")
	f.BoolVar(&flagDebug, "debug", false, "log intermediate decisions (routing, engine choice, dict hit/miss)")
	f.BoolVarP(&flagSpeak, "speak", "s", false, "speak the foreign side of the result aloud (free TTS)")
	f.StringVar(&flagSpeakLang, "speak-lang", "", "force the spoken language (e.g. en, zh-TW)")

	root.SuggestionsMinimumDistance = 2
	root.AddCommand(newConfigCmd(), newLangCmd(), newDefineCmd(), newHistoryCmd(), newInitCmd(), newDictCmd(), newSpeakCmd())
	return root
}

// Execute runs the root command with the given (cancellable) context.
func Execute(ctx context.Context) error {
	return NewRootCmd().ExecuteContext(ctx)
}

// overrides collects the explicitly-set flag values.
func overrides() config.Overrides {
	return config.Overrides{
		Source:       flagFrom,
		Target:       flagTo,
		Engine:       flagEngine,
		Provider:     flagProvider,
		Model:        flagModel,
		Tier:         flagTier,
		Preset:       flagPreset,
		Instructions: flagInstructions,
		Pair:         flagPair,
		PairWith:     flagPairWith,
		Learn:        flagLearn,
		Debug:        flagDebug,
	}
}

// invocationMode reports whether this run is the one-shot CLI (arguments or piped
// stdin) or the interactive TUI (a TTY with no arguments). It mirrors the dispatch
// switch in runRoot and is computed before Resolve so the [cli]/[tui] overlay applies.
func invocationMode(args []string) config.Mode {
	if len(args) > 0 {
		return config.ModeCLI
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return config.ModeCLI
	}
	return config.ModeTUI
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

	res := cfg.Resolve(overrides(), invocationMode(args))
	if res.Debug {
		// The one-shot CLI logs to stderr; the TUI logs to a file, since its
		// alt-screen would be corrupted by stderr writes.
		if invocationMode(args) == config.ModeCLI {
			debug.Enable(os.Stderr)
		} else if w := openDebugLog(); w != nil {
			debug.Enable(w)
		}
	}
	applyLastPair(cfg, &res) // remember_last_pair: seed source/target from state
	src, tgt := resolvePair(res.Source, res.Target)
	pairWith := ""
	if res.Pair {
		pwm, _ := lang.Resolve(res.PairWith)
		pairWith = pwm.Code
	}
	// Learn mode is bidirectional; ensure a distinct "away" (foreign) language so
	// direction routing has two sides to choose between.
	if res.Learn && (pairWith == "" || strings.EqualFold(pairWith, tgt)) {
		pairWith = defaultAway(tgt)
	}
	debug.Logf("resolved: engine=%s tier=%s preset=%s source=%s target=%s pair=%v pair_with=%s learn=%v",
		res.Engine, res.Tier, res.Preset, src, tgt, res.Pair, pairWith, res.Learn)
	if res.Pair && !res.Learn && (pairWith == "" || strings.EqualFold(pairWith, tgt)) {
		fmt.Fprintf(os.Stderr, "translate: warning: pair mode is on but pair-with (%q) equals the target (%q) — pair mode is a no-op; set a different pair_with (run `translate init`)\n", pairWith, tgt)
	}

	if res.Provider == nil && res.Engine != "auto" {
		return fmt.Errorf("no provider configured; check %s", config.Path())
	}
	eng, err := buildEngine(res)
	if err != nil {
		return err
	}

	// The one-shot CLI path routes a learn request through a bare LLM engine
	// (bypassing smart-auto/dictionary); the TUI keeps its normal primary engine and
	// toggles learn at runtime against Params.LearnEngine.
	learnCLI := res.Learn && invocationMode(args) == config.ModeCLI
	if learnCLI && res.Provider == nil {
		return fmt.Errorf("learn mode requires an LLM provider; check %s", config.Path())
	}
	oneShotEng := eng
	if learnCLI {
		oneShotEng = learnEngineFromConfig(res)
	}

	st := openStore(cfg)
	if st != nil {
		defer st.Close()
	}

	switch {
	case len(args) > 0:
		text := strings.Join(args, " ")
		effTgt := effectiveTarget(res.Pair, tgt, pairWith, text)
		r, err := oneShot(ctx, oneShotEng, text, src, effTgt, res.Stream, res.Preset, res.Instructions, res.Pair, tgt, pairWith, res.Learn)
		if err != nil {
			return err
		}
		recordAndRemember(ctx, st, cfg, r, text, src, effTgt, tgt)
		if shouldSpeak(cfg) {
			speakResult(ctx, cfg, r, text, src, effTgt, speakForeign(cfg, pairWith))
		}
		return nil

	case !term.IsTerminal(int(os.Stdin.Fd())):
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		// Strip ANSI/SGR escapes so colored piped input (e.g. `tldr rg | translate`)
		// never pollutes the prompt. The raw bytes are retained only for --bilingual,
		// which needs the original styling for display.
		text := strings.TrimSpace(bitext.Strip(string(b)))
		if text == "" {
			return fmt.Errorf("no input on stdin")
		}
		// Bilingual is a multi-block reading view; --json/--learn keep their own
		// structured output and take precedence.
		if flagBilingual && !flagJSON && !res.Learn {
			return runBilingual(ctx, oneShotEng, string(b), src, tgt, res.Instructions)
		}
		effTgt := effectiveTarget(res.Pair, tgt, pairWith, text)
		r, err := oneShot(ctx, oneShotEng, text, src, effTgt, res.Stream, res.Preset, res.Instructions, res.Pair, tgt, pairWith, res.Learn)
		if err != nil {
			return err
		}
		recordAndRemember(ctx, st, cfg, r, text, src, effTgt, tgt)
		if shouldSpeak(cfg) {
			speakResult(ctx, cfg, r, text, src, effTgt, speakForeign(cfg, pairWith))
		}
		return nil

	default:
		return runTUI(ctx, eng, res, st, src, tgt, pairWith)
	}
}

// effectiveTarget applies pair mode: home-language input → away, else → home.
func effectiveTarget(pair bool, home, away, text string) string {
	if pair && away != "" {
		t := lang.PairTarget(home, away, text)
		debug.Logf("pair route: home=%s away=%s text=%q → target=%s", home, away, clip(text, 30), t)
		return t
	}
	return home
}

// clip shortens a string for a single-line debug label.
func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// openDebugLog opens (append) the TUI debug log file, creating the state dir.
// Returns nil on failure (debug simply stays off).
func openDebugLog() io.Writer {
	if err := xdgpath.EnsureDirs(); err != nil {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(xdgpath.StateDir(), "debug.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil
	}
	return f
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
		debug.Logf("remember_last_pair: source ← %s (from state)", stt.Source)
	}
	if flagTo == "" && os.Getenv("TRANSLATE_TARGET") == "" && stt.Target != "" {
		res.Target = stt.Target
		debug.Logf("remember_last_pair: target ← %s (from state)", stt.Target)
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
//
// target is the effective target actually used (for history); rememberTarget is
// the stable home target (== target unless pair mode redirected this one input),
// so persisting it keeps pair mode from drifting its home to the away language.
func recordAndRemember(ctx context.Context, st store.Store, cfg *config.Config, res *engine.TranslateResult, input, source, target, rememberTarget string) {
	if st != nil {
		_, _ = st.Add(ctx, toRecord(res, input, source, target))
	}
	if cfg.General.RememberLastPair {
		saveLastPair(source, rememberTarget, res.Engine)
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
func runTUI(ctx context.Context, eng engine.Engine, res config.Resolved, st store.Store, source, target, pairWith string) error {
	// A model source for the ^p model picker: the resolved LLM provider (if any).
	var modelSrc engine.ModelLister
	modelProvider := ""
	if res.Provider != nil {
		modelSrc = llmFromProvider(res.Provider, res.Model)
		modelProvider = res.Provider.Name
	}
	// Learn mode (^n) runs against a bare LLM engine; nil when no provider is
	// configured, in which case the toggle refuses.
	var learnEng engine.Engine
	if res.Provider != nil {
		learnEng = learnEngineFromConfig(res)
	}
	p := tui.Params{
		Engines:       buildEngineSet(res, eng),
		ModelSource:   modelSrc,
		ModelProvider: modelProvider,
		Store:         st,
		Source:        source,
		Target:        target,
		Pair:          res.Pair,
		PairWith:      pairWith,
		Learn:         res.Learn,
		LearnEngine:   learnEng,
		Model:         res.Model,
		Preset:        res.Preset,
		Instructions:  res.Instructions,
		Live:          res.LiveTranslate,
		DebounceMs:    res.DebounceMs,
		Speaker:       tuiSpeaker(res.Cfg),
		Foreign:       speakForeign(res.Cfg, pairWith),
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
func oneShot(ctx context.Context, eng engine.Engine, text, source, target string, streamPref bool, preset, instructions string, pair bool, pairHome, pairAway string, learn bool) (*engine.TranslateResult, error) {
	stdoutTTY := term.IsTerminal(int(os.Stdout.Fd()))
	stream := streamPref && stdoutTTY && !flagJSON && !learn // learn output is structured (parsed at done)

	req := engine.Request{
		Text:     text,
		Source:   source,
		Target:   target,
		Mode:     engine.ModeTranslate,
		Stream:   stream,
		Preset:   preset,
		Extra:    instructions,
		Pair:     pair,
		PairHome: pairHome,
		PairAway: pairAway,
		Learn:    learn,
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
	if len(res.Warnings) > 0 && res.Learn == nil {
		fmt.Fprintf(os.Stderr, "translate: used %q (switch with --engine, or check the model/provider)\n", res.Engine)
	}

	switch {
	case flagJSON:
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			return nil, err
		}
	case res.Learn != nil:
		fmt.Print(renderLearnCLI(res))
	case stream:
		fmt.Println() // newline after the streamed tokens
	default:
		fmt.Println(res.Translation)
	}
	return res, nil
}

// runBilingual renders the "immersive" bilingual view for piped input: every
// original block is printed verbatim (ANSI/color intact) and a translation is
// shown beneath each prose block. Indented command/code blocks are echoed
// untranslated. Prose blocks translate concurrently (bounded) into target; pair
// and learn routing are intentionally out of scope for this reading mode.
func runBilingual(ctx context.Context, eng engine.Engine, raw, source, target, instructions string) error {
	blocks := bitext.Split(raw)
	if len(blocks) == 0 {
		return fmt.Errorf("no input on stdin")
	}

	translations := make(map[int]string)
	errs := make(map[int]error)
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5) // bound concurrent LLM calls

	for i, blk := range blocks {
		if blk.Kind != bitext.Prose {
			continue
		}
		wg.Add(1)
		go func(i int, plain string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Force concise (Stream:false, Preset:"") so each block maps to exactly
			// one translation — contextual/dictionary presets would reshape output.
			r, err := translateOnce(ctx, eng, engine.Request{
				Text:   plain,
				Source: source,
				Target: target,
				Mode:   engine.ModeTranslate,
				Extra:  instructions,
			})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs[i] = err
				return
			}
			tr := strings.TrimSpace(r.Translation)
			// Drop echoes (proper nouns / already-target text): nothing to add beneath.
			if tr != "" && !strings.EqualFold(tr, strings.TrimSpace(plain)) {
				translations[i] = tr
			}
		}(i, blk.Plain)
	}
	wg.Wait()

	dim := dimFunc(term.IsTerminal(int(os.Stdout.Fd())))
	fmt.Print(bitext.Render(blocks, translations, dim))

	// Never fail silently: report untranslated blocks (originals still printed).
	if len(errs) > 0 {
		var sample error
		for _, e := range errs {
			sample = e
			break
		}
		fmt.Fprintf(os.Stderr, "translate: warning: %d block(s) not translated (%v)\n", len(errs), sample)
	}
	return nil
}

// translateOnce runs a single non-streaming translation and returns the result,
// draining the engine channel with no token callback. It is the printing-free core
// shared by bilingual mode.
func translateOnce(ctx context.Context, eng engine.Engine, req engine.Request) (*engine.TranslateResult, error) {
	ch, err := eng.Translate(ctx, req)
	if err != nil {
		return nil, err
	}
	return engine.Drain(ch, nil)
}

// dimFunc returns the styler for bilingual translation lines. It dims (grey) only
// when stdout is a TTY and NO_COLOR is unset, so piping the output onward stays
// clean; otherwise it is the identity function. The original block's own ANSI is
// never touched — only our added translation lines are styled.
func dimFunc(stdoutTTY bool) func(string) string {
	if !stdoutTTY || os.Getenv("NO_COLOR") != "" {
		return func(s string) string { return s }
	}
	style := lg.NewStyle().Foreground(lg.Color("#6C6C6C"))
	return func(s string) string { return style.Render(s) }
}

// defaultAway picks the "away" (foreign) language for learn/pair mode when none is
// configured: English speakers default to learning zh-TW, everyone else to en. It
// mirrors the TUI ^g heuristic so the CLI and TUI agree.
func defaultAway(home string) string {
	if strings.HasPrefix(strings.ToLower(home), "en") {
		return "zh-TW"
	}
	return "en"
}

// renderLearnCLI formats a learn-mode result as plain text (no ANSI, pipe-safe).
func renderLearnCLI(res *engine.TranslateResult) string {
	l := res.Learn
	if l == nil {
		return res.Translation + "\n"
	}
	var b strings.Builder
	if l.Direction == "correct" {
		corrected := strings.TrimSpace(l.Corrected)
		if corrected == "" {
			corrected = res.Translation
		}
		b.WriteString("✔ " + corrected + "\n")
		if s := strings.TrimSpace(l.Translation); s != "" {
			b.WriteString("  ↳ " + s + "\n")
		}
		for _, is := range l.Issues {
			frag := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(is.Span+" → "+is.Fix, " → "), " → "))
			expl := strings.TrimSpace(is.Explanation)
			line := expl
			switch {
			case frag != "" && expl != "":
				line = frag + ": " + expl
			case frag != "":
				line = frag
			}
			if line != "" {
				b.WriteString("  ✎ " + line + "\n")
			}
		}
	} else {
		tr := strings.TrimSpace(l.Translation)
		if tr == "" {
			tr = res.Translation
		}
		b.WriteString(tr + "\n")
		for _, v := range l.Vocab {
			head := "  • " + v.Term
			if v.Pos != "" {
				head += " (" + v.Pos + ")"
			}
			if v.Phonetic != "" {
				head += " " + v.Phonetic
			}
			if v.Meaning != "" {
				head += " — " + v.Meaning
			}
			b.WriteString(head + "\n")
		}
		for _, ex := range l.Examples {
			b.WriteString("  ✎ " + ex.Foreign + "\n")
			if s := strings.TrimSpace(ex.Native); s != "" {
				b.WriteString("    ↳ " + s + "\n")
			}
		}
	}
	if s := strings.TrimSpace(l.Notes); s != "" {
		b.WriteString("  ⓘ " + s + "\n")
	}
	return b.String()
}
