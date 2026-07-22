package appcore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/daviddwlee84/translate/internal/config"
	"github.com/daviddwlee84/translate/internal/engine"
	"github.com/daviddwlee84/translate/internal/store"
)

// stubEngine is a canned engine.Engine for tests: it records the last request and
// replays a fixed sequence of chunks (or a synchronous setup error).
type stubEngine struct {
	name    string
	chunks  []engine.Chunk
	setErr  error
	lastReq engine.Request
}

func (s *stubEngine) Name() string                                   { return s.name }
func (s *stubEngine) Supports(engine.Mode) bool                      { return true }
func (s *stubEngine) Available(context.Context) bool                 { return true }
func (s *stubEngine) Detect(context.Context, string) (string, error) { return "", nil }

func (s *stubEngine) Translate(_ context.Context, req engine.Request) (<-chan engine.Chunk, error) {
	s.lastReq = req
	if s.setErr != nil {
		return nil, s.setErr
	}
	ch := make(chan engine.Chunk, len(s.chunks))
	for _, c := range s.chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func done(res *engine.TranslateResult) engine.Chunk {
	return engine.Chunk{Kind: engine.ChunkDone, Result: res}
}
func tok(t string) engine.Chunk { return engine.Chunk{Kind: engine.ChunkToken, Text: t} }

func newTestService(t *testing.T, cfg *config.Config, trans, def engine.Engine, withStore bool) *Service {
	t.Helper()
	res := cfg.Resolve(config.Overrides{}, config.ModeCLI)
	var st store.Store
	if withStore {
		var err error
		st, err = store.OpenJSONL(filepath.Join(t.TempDir(), "history.jsonl"))
		if err != nil {
			t.Fatalf("open store: %v", err)
		}
	}
	return newService(cfg, res, trans, def, st)
}

func TestServiceTranslateRecordsHistory(t *testing.T) {
	trans := &stubEngine{name: "stub", chunks: []engine.Chunk{
		tok("你"), tok("好"),
		done(&engine.TranslateResult{Translation: "你好", Target: "zh", Engine: "stub"}),
	}}
	svc := newTestService(t, config.Default(), trans, &stubEngine{name: "dict"}, true)

	res, err := svc.Translate(context.Background(), Params{Text: "hello", Target: "zh"})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if res.Translation != "你好" {
		t.Fatalf("translation = %q, want 你好", res.Translation)
	}
	if trans.lastReq.Target != "zh" || trans.lastReq.Source != "auto" || trans.lastReq.Mode != engine.ModeTranslate {
		t.Fatalf("request = %+v, want target=zh source=auto mode=translate", trans.lastReq)
	}

	recent, err := svc.HistoryRecent(context.Background(), 10)
	if err != nil {
		t.Fatalf("HistoryRecent: %v", err)
	}
	if len(recent) != 1 || recent[0].Input != "hello" || recent[0].Output != "你好" {
		t.Fatalf("history = %+v, want one record hello→你好", recent)
	}
}

func TestServiceTruncatedNotRecorded(t *testing.T) {
	trans := &stubEngine{name: "stub", chunks: []engine.Chunk{
		done(&engine.TranslateResult{Translation: "partial", Truncated: true}),
	}}
	svc := newTestService(t, config.Default(), trans, &stubEngine{}, true)

	if _, err := svc.Translate(context.Background(), Params{Text: "hello"}); err != nil {
		t.Fatalf("Translate: %v", err)
	}
	recent, _ := svc.HistoryRecent(context.Background(), 10)
	if len(recent) != 0 {
		t.Fatalf("history = %+v, want empty (truncated result must not be recorded)", recent)
	}
}

func TestServiceStreamDeliversTokens(t *testing.T) {
	trans := &stubEngine{name: "stub", chunks: []engine.Chunk{
		tok("a"), tok("b"), tok("c"),
		done(&engine.TranslateResult{Translation: "abc"}),
	}}
	svc := newTestService(t, config.Default(), trans, &stubEngine{}, false)

	var got []string
	res, err := svc.TranslateStream(context.Background(), Params{Text: "x"}, func(s string) { got = append(got, s) })
	if err != nil {
		t.Fatalf("TranslateStream: %v", err)
	}
	if res.Translation != "abc" {
		t.Fatalf("translation = %q, want abc", res.Translation)
	}
	if len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Fatalf("tokens = %v, want [a b c]", got)
	}
	if !trans.lastReq.Stream {
		t.Fatal("expected Stream=true on the request")
	}
}

func TestServiceTranslateErrorPropagates(t *testing.T) {
	trans := &stubEngine{name: "stub", chunks: []engine.Chunk{
		{Kind: engine.ChunkError, Err: errors.New("boom")},
	}}
	svc := newTestService(t, config.Default(), trans, &stubEngine{}, true)

	if _, err := svc.Translate(context.Background(), Params{Text: "hello"}); err == nil {
		t.Fatal("expected error, got nil")
	}
	recent, _ := svc.HistoryRecent(context.Background(), 10)
	if len(recent) != 0 {
		t.Fatalf("history = %+v, want empty after error", recent)
	}
}

func TestServicePairRouting(t *testing.T) {
	cfg := config.Default()
	cfg.General.Pair = true
	cfg.General.PairWith = "zh" // home=en, away=zh
	trans := &stubEngine{name: "stub", chunks: []engine.Chunk{done(&engine.TranslateResult{Translation: "x"})}}
	svc := newTestService(t, cfg, trans, &stubEngine{}, false)

	// Latin (home-language) input routes to the away CJK language.
	if _, err := svc.Translate(context.Background(), Params{Text: "hello"}); err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if trans.lastReq.Target != "zh" {
		t.Fatalf("en input target = %q, want zh (route to away)", trans.lastReq.Target)
	}
	if !trans.lastReq.Pair || trans.lastReq.PairHome != "en" || trans.lastReq.PairAway != "zh" {
		t.Fatalf("pair fields = %+v, want Pair=true home=en away=zh", trans.lastReq)
	}

	// CJK (away-language) input routes back to the home Latin language.
	if _, err := svc.Translate(context.Background(), Params{Text: "你好世界"}); err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if trans.lastReq.Target != "en" {
		t.Fatalf("zh input target = %q, want en (route to home)", trans.lastReq.Target)
	}
}

func TestServiceDefineNotRecorded(t *testing.T) {
	def := &stubEngine{name: "dict", chunks: []engine.Chunk{
		done(&engine.TranslateResult{Translation: "gloss", Dictionary: &engine.DictEntry{Word: "hello"}}),
	}}
	svc := newTestService(t, config.Default(), &stubEngine{}, def, true)

	res, err := svc.Define(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Define: %v", err)
	}
	if res.Dictionary == nil || res.Dictionary.Word != "hello" {
		t.Fatalf("define result = %+v, want dictionary for hello", res)
	}
	if def.lastReq.Mode != engine.ModeDict {
		t.Fatalf("define request mode = %v, want ModeDict", def.lastReq.Mode)
	}
	recent, _ := svc.HistoryRecent(context.Background(), 10)
	if len(recent) != 0 {
		t.Fatalf("history = %+v, want empty (define is not recorded)", recent)
	}
}
