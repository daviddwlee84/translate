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

	"github.com/daviddwlee84/translate/internal/appcore"
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
	flagTo            string
	flagFrom          string
	flagModel         string
	flagProvider      string
	flagEngine        string
	flagTier          string
	flagPreset        string
	flagInstructions  string
	flagPair          bool
	flagPairWith      string
	flagLearn         bool
	flagBilingual     bool
	flagBilingualMode string
	flagJSON          bool
	flagStream        bool
	flagNoHistory     bool
	flagDebug         bool
	flagSpeak         bool
	flagSpeakLang     string
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
	f.StringVar(&flagBilingualMode, "bilingual-mode", "doc", "bilingual strategy: doc (context-aware, one LLM call) | blocks (per-block)")
	f.BoolVar(&flagJSON, "json", false, "emit the full result as JSON")
	f.BoolVar(&flagStream, "stream", false, "force token streaming to stdout even when it is not a TTY (for piped consumers, e.g. the Raycast extension)")
	f.BoolVar(&flagNoHistory, "no-history", false, "do not record this translation in history")
	f.BoolVar(&flagDebug, "debug", false, "log intermediate decisions (routing, engine choice, dict hit/miss)")
	f.BoolVarP(&flagSpeak, "speak", "s", false, "speak the foreign side of the result aloud (free TTS)")
	f.StringVar(&flagSpeakLang, "speak-lang", "", "force the spoken language (e.g. en, zh-TW)")

	root.SuggestionsMinimumDistance = 2
	root.AddCommand(newConfigCmd(), newLangCmd(), newDefineCmd(), newHistoryCmd(), newInitCmd(), newDictCmd(), newSpeakCmd(), newServeCmd(), newMcpCmd())
	return root
}

// Execute runs the root command with the given (cancellable) context.
func Execute(ctx context.Context) error {
	// Stamp the writing app version into any config Save this run makes.
	config.Generator = shortVersion()
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
	} else if cfg.Outdated() {
		// Schema bumps so far are additive: Load already filled any new fields with
		// their Default() values in memory, so re-saving materializes them on disk
		// (every existing value preserved) and re-stamps the schema — no wizard
		// needed. A genuinely breaking change would instead need an explicit
		// migration here before the re-save.
		from := cfg.Schema
		if err := config.Save(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "translate: config is outdated but auto-upgrade failed (%v) — run `translate init`\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "translate: upgraded config schema v%d→v%d (new settings use defaults; `translate init` to review)\n", from, config.SchemaVersion)
		}
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
	eng, err := appcore.BuildEngine(res)
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
		oneShotEng = appcore.LearnEngineFromConfig(res)
	}

	st := openStore(cfg)
	if st != nil {
		defer st.Close()
	}

	switch {
	case len(args) > 0:
		text := strings.Join(args, " ")
		effTgt := appcore.EffectiveTarget(res.Pair, tgt, pairWith, text)
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
			// doc mode needs an LLM (structured JSON, empty Text confuses
			// smartauto/chain routing) — build a bare LLM like learn mode; nil when
			// no provider, in which case runBilingual falls back to per-block.
			var docEng engine.Engine
			if res.Provider != nil {
				docEng = appcore.LearnEngineFromConfig(res)
			}
			return runBilingual(ctx, oneShotEng, docEng, string(b), src, tgt, res.Instructions, flagBilingualMode)
		}
		effTgt := appcore.EffectiveTarget(res.Pair, tgt, pairWith, text)
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
	// Pair (and learn) mode anchors BOTH languages from config: Target is the home
	// language, PairWith is the away language, and routing is decided per input. A
	// remembered target must not override the home here — if the last session
	// happened to translate into the away language, restoring it as the home makes
	// home == away and pair mode collapses to a no-op (the "en⇄en" bug). Source is
	// always auto-detected in pair mode too, so skip the whole override.
	if res.Pair {
		debug.Logf("remember_last_pair: skipped (pair mode anchors languages from config)")
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
		_, _ = st.Add(ctx, appcore.ToRecord(res, input, source, target))
	}
	if cfg.General.RememberLastPair {
		saveLastPair(source, rememberTarget, res.Engine)
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
		modelSrc = appcore.LLMFromProvider(res.Provider, res.Model)
		modelProvider = res.Provider.Name
	}
	// Learn mode (^n) runs against a bare LLM engine; nil when no provider is
	// configured, in which case the toggle refuses.
	var learnEng engine.Engine
	if res.Provider != nil {
		learnEng = appcore.LearnEngineFromConfig(res)
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
	// Stream when the config asks for it on a TTY, or when --stream forces it (a
	// piped consumer like the Raycast extension). --json/--learn are structured
	// and never stream.
	stream := (flagStream || (streamPref && stdoutTTY)) && !flagJSON && !learn

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

	// streamed tracks whether any visible token actually reached stdout. We may
	// request streaming (stream == true) yet the engine that answers ignores it and
	// returns a single terminal result with zero tokens — every non-streaming engine
	// does (the dictionary/smart-dict single-word path, google, a provider that fell
	// back to a non-streaming completion). In that case nothing was printed during
	// draining, so the final result must still be emitted below.
	var onTok func(string)
	streamed := false
	if stream {
		onTok = func(t string) {
			if t != "" {
				streamed = true
			}
			fmt.Print(t)
		}
	}
	res, err := engine.Drain(ch, onTok)
	if err != nil {
		if streamed {
			fmt.Println() // terminate any partial streamed line before the error surfaces
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
	case streamed:
		fmt.Println() // newline after the streamed tokens
	default:
		// Piped output, or a non-streaming engine answered a stream request with no
		// tokens (dictionary single-word, google, …): emit the full result now.
		fmt.Println(res.Translation)
	}
	return res, nil
}

// runBilingual renders the "immersive" bilingual view for piped input: every
// original block is printed verbatim (ANSI/color intact) and a translation is shown
// beneath each prose block; indented command/code blocks are echoed untranslated.
// mode "doc" (default) translates the whole document in one context-aware call via
// docEng (a bare LLM); "blocks" (or a doc-mode fallback) translates each prose block
// in isolation via blockEng. Pair and learn routing are out of scope for this view.
func runBilingual(ctx context.Context, blockEng, docEng engine.Engine, raw, source, target, instructions, mode string) error {
	blocks := bitext.Split(raw)
	if len(blocks) == 0 {
		return fmt.Errorf("no input on stdin")
	}

	var translations map[int]string
	switch mode {
	case "blocks":
		translations = bilingualBlocks(ctx, blockEng, blocks, source, target, instructions)
	default: // "doc"
		if docEng != nil {
			if t, ok := bilingualDoc(ctx, docEng, blocks, source, target, instructions); ok {
				translations = t
			}
		}
		if translations == nil { // no LLM provider, or the doc call failed/was unparseable
			fmt.Fprintln(os.Stderr, "translate: bilingual: doc mode unavailable, using per-block")
			translations = bilingualBlocks(ctx, blockEng, blocks, source, target, instructions)
		}
	}

	dim := dimFunc(term.IsTerminal(int(os.Stdout.Fd())))
	fmt.Print(bitext.Render(blocks, translations, dim))
	return nil
}

// bilingualBlocks translates each prose block in isolation (concurrent, bounded).
// The original strategy: works with any engine, but has no cross-block context.
func bilingualBlocks(ctx context.Context, eng engine.Engine, blocks []bitext.Block, source, target, instructions string) map[int]string {
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

	// Never fail silently: report untranslated blocks (originals still printed).
	if len(errs) > 0 {
		var sample error
		for _, e := range errs {
			sample = e
			break
		}
		fmt.Fprintf(os.Stderr, "translate: warning: %d block(s) not translated (%v)\n", len(errs), sample)
	}
	return translations
}

// bilingualDoc translates the whole document in ONE context-aware structured call:
// the model sees every block (prose + code as context) and returns a JSON map of
// prose-segment number → translation. Returns ok=false when structured output isn't
// available (no LLM, error, or unparseable/empty reply) so the caller falls back.
func bilingualDoc(ctx context.Context, eng engine.Engine, blocks []bitext.Block, source, target, instructions string) (map[int]string, bool) {
	var segs []engine.Segment
	proseBlock := make(map[int]int) // prose-segment number (1-based) → block index
	n := 0
	for i, blk := range blocks {
		switch blk.Kind {
		case bitext.Prose:
			n++
			proseBlock[n] = i
			segs = append(segs, engine.Segment{Text: blk.Plain})
		case bitext.Code:
			segs = append(segs, engine.Segment{Text: blk.Plain, Code: true})
		}
	}
	if n == 0 {
		return map[int]string{}, true // nothing to translate
	}

	r, err := translateOnce(ctx, eng, engine.Request{
		Bilingual: true,
		Segments:  segs,
		Source:    source,
		Target:    target,
		Mode:      engine.ModeTranslate,
		Extra:     instructions,
	})
	if err != nil {
		debug.Logf("bilingual doc: LLM call failed: %v", err)
		return nil, false
	}
	if r == nil || len(r.Bilingual) == 0 {
		warn := ""
		if r != nil && len(r.Warnings) > 0 {
			warn = r.Warnings[0]
		}
		debug.Logf("bilingual doc: empty/unparseable structured result (warnings: %q)", warn)
		return nil, false
	}

	translations := make(map[int]string)
	for num, tr := range r.Bilingual {
		bi, ok := proseBlock[num]
		if !ok {
			continue
		}
		t := strings.TrimSpace(tr)
		if t != "" && !strings.EqualFold(t, strings.TrimSpace(blocks[bi].Plain)) {
			translations[bi] = t
		}
	}
	return translations, true
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
