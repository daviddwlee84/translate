package appcore

import (
	"context"
	"fmt"

	"github.com/daviddwlee84/translate/internal/config"
	"github.com/daviddwlee84/translate/internal/engine"
	"github.com/daviddwlee84/translate/internal/lang"
	"github.com/daviddwlee84/translate/internal/store"
	"github.com/daviddwlee84/translate/internal/xdgpath"
)

// Service is the warm, transport-agnostic core: it builds the translate/define
// engines once at startup and holds the history store for the process lifetime.
// A single Service is safe to share across many goroutines — the engines are
// concurrency-safe and the store serializes its own writes.
//
// The Service deliberately never touches state.json (the CLI's remembered pair):
// a long-lived server must not fight the interactive CLI over that file.
type Service struct {
	cfg      *config.Config
	res      config.Resolved
	trans    engine.Engine // warm translate engine
	define   engine.Engine // warm define engine
	store    store.Store   // shared history; nil when disabled / NoHistory
	provider string        // resolved provider name (for per-request model overrides)

	source string // resolved default source (usually "auto")
	home   string // resolved home target
	away   string // resolved pair "away" language; "" when pair is off
	pair   bool
}

// Options tunes Service construction.
type Options struct {
	NoHistory bool // do not open/record history
}

// Params is one translation request. Empty fields fall back to the Service
// defaults resolved at startup; Pair is a tri-state (nil => use the default).
type Params struct {
	Text         string
	Source       string // "" => service default (usually "auto")
	Target       string // "" => service home target
	Preset       string // "" => service default
	Instructions string // "" => service default
	Model        string // "" => the warm engine's model
	MaxAlts      int    // 0 => engine default
	Pair         *bool  // nil => service default; non-nil overrides
}

// NewService resolves config (flags empty, env honored) once, warms both engines,
// and opens the history store. It fails fast on a misconfigured provider, mirroring
// the CLI's guard in runRoot.
func NewService(cfg *config.Config, opt Options) (*Service, error) {
	res := cfg.Resolve(config.Overrides{}, config.ModeCLI)
	if res.Provider == nil && res.Engine != "auto" {
		return nil, fmt.Errorf("no provider configured; check %s", config.Path())
	}
	trans, err := BuildEngine(res)
	if err != nil {
		return nil, err
	}
	def := DefineEngine(res, false, false)
	st := openStore(cfg, opt.NoHistory)
	return newService(cfg, res, trans, def, st), nil
}

// newService assembles a Service from already-built parts. It is the unexported
// seam used by tests to inject stub engines and an in-memory store (no network).
func newService(cfg *config.Config, res config.Resolved, trans, def engine.Engine, st store.Store) *Service {
	source, home, away, pair := resolveRouting(res)
	provider := ""
	if res.Provider != nil {
		provider = res.Provider.Name
	}
	return &Service{
		cfg: cfg, res: res, trans: trans, define: def, store: st,
		provider: provider, source: source, home: home, away: away, pair: pair,
	}
}

// resolveRouting fuzzy-resolves the source/target/pair-with languages the same way
// the CLI does (resolvePair + lang.Resolve), so "chinese" → "zh" etc.
func resolveRouting(res config.Resolved) (source, home, away string, pair bool) {
	sm, _ := lang.Resolve(res.Source)
	tm, _ := lang.Resolve(res.Target)
	source, home = sm.Code, tm.Code
	pair = res.Pair
	if res.Pair {
		pwm, _ := lang.Resolve(res.PairWith)
		away = pwm.Code
	}
	return source, home, away, pair
}

// Translate performs a non-streaming translation and records it in history.
func (s *Service) Translate(ctx context.Context, p Params) (*engine.TranslateResult, error) {
	return s.translate(ctx, p, false, nil)
}

// TranslateStream performs a streaming translation, delivering tokens to onToken
// as they arrive, and records the final result in history.
func (s *Service) TranslateStream(ctx context.Context, p Params, onToken func(string)) (*engine.TranslateResult, error) {
	return s.translate(ctx, p, true, onToken)
}

func (s *Service) translate(ctx context.Context, p Params, stream bool, onToken func(string)) (*engine.TranslateResult, error) {
	req := s.buildRequest(p, stream)
	ch, err := s.trans.Translate(ctx, req)
	if err != nil {
		return nil, err
	}
	res, err := engine.Drain(ch, onToken)
	if err != nil {
		return nil, err
	}
	s.record(ctx, res, req.Text, req.Source, req.Target)
	return res, nil
}

// buildRequest maps Params onto an engine.Request, mirroring the CLI's oneShot +
// EffectiveTarget so the API and CLI route identically.
func (s *Service) buildRequest(p Params, stream bool) engine.Request {
	source := firstNonEmpty(p.Source, s.source)
	home := firstNonEmpty(p.Target, s.home)
	pair := s.pair
	if p.Pair != nil {
		pair = *p.Pair
	}
	target := EffectiveTarget(pair, home, s.away, p.Text)

	req := engine.Request{
		Text:     p.Text,
		Source:   source,
		Target:   target,
		Mode:     engine.ModeTranslate,
		Stream:   stream,
		MaxAlts:  p.MaxAlts,
		Preset:   firstNonEmpty(p.Preset, s.res.Preset),
		Extra:    firstNonEmpty(p.Instructions, s.res.Instructions),
		Pair:     pair,
		PairHome: home,
		PairAway: s.away,
	}
	if p.Model != "" {
		// Scope the override to the resolved provider so a copilot model id never
		// leaks into an Ollama fallback (see engine.Request.ModelProvider).
		req.Model = p.Model
		req.ModelProvider = s.provider
	}
	return req
}

// Define performs a dictionary/definition lookup (not recorded in history, matching
// the CLI's `translate define`).
func (s *Service) Define(ctx context.Context, word string) (*engine.TranslateResult, error) {
	ch, err := s.define.Translate(ctx, engine.Request{Text: word, Mode: engine.ModeDict, Stream: false})
	if err != nil {
		return nil, err
	}
	return engine.Drain(ch, nil)
}

// HistoryRecent returns up to limit history records, newest first (empty when
// history is disabled).
func (s *Service) HistoryRecent(ctx context.Context, limit int) ([]store.Record, error) {
	if s.store == nil {
		return nil, nil
	}
	return s.store.Recent(ctx, limit)
}

// HistorySearch fuzzy-searches history (empty when history is disabled).
func (s *Service) HistorySearch(ctx context.Context, query string, limit int) ([]store.Record, error) {
	if s.store == nil {
		return nil, nil
	}
	return s.store.Search(ctx, query, limit)
}

// Close releases the history store.
func (s *Service) Close() error {
	if s.store != nil {
		return s.store.Close()
	}
	return nil
}

// record persists a successful, complete result. Truncated results (a stream cut
// short) are never recorded, so a partial translation is never cached as complete.
func (s *Service) record(ctx context.Context, res *engine.TranslateResult, input, source, target string) {
	if s.store == nil || res == nil || res.Truncated {
		return
	}
	_, _ = s.store.Add(ctx, ToRecord(res, input, source, target))
}

// openStore opens the history store, or returns nil when history is disabled or
// suppressed. Unlike the CLI helper it stays silent on failure (the server logs
// separately); a nil store simply disables history for this process.
func openStore(cfg *config.Config, noHistory bool) store.Store {
	if !cfg.History.Enabled || noHistory {
		return nil
	}
	path := cfg.History.Path
	if path == "" {
		path = xdgpath.HistoryFile()
	}
	st, err := store.OpenJSONL(path)
	if err != nil {
		return nil
	}
	return st
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
