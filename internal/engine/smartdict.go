package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/daviddwlee84/translate/internal/debug"
	"github.com/daviddwlee84/translate/internal/lang"
)

// SmartDictConfig parameterizes the smart-dict engine.
type SmartDictConfig struct {
	// CloseDistance is the English edit distance at/below which a fuzzy "did you
	// mean" is treated as a likely typo and returned as-is; a farther match (and
	// any hard miss) triggers the LLM fallback. 0 disables the typo shortcut, so
	// every non-exact lookup falls back.
	CloseDistance int
	// Preset is the LLM prompt style used for the fallback; "" => PresetDictionary
	// (a gloss plus example sentences).
	Preset string
}

// SmartDictEngine composes a dictionary engine with an LLM: it answers from the
// dictionary when it can, and falls back to the LLM when the word is missing or the
// fuzzy match is too far off. It is a distinct, selectable engine — the plain
// dictionary engine is left untouched.
//
// This is the first engine to branch on a produced result's quality (the Chain
// only fails over on hard errors), so the fallback always stamps a Warning: the
// downgrade from an offline lookup to an LLM answer is never silent.
type SmartDictEngine struct {
	dict Engine
	llm  Engine
	cfg  SmartDictConfig
}

// NewSmartDict builds a smart-dict engine over a dictionary and an LLM backend.
func NewSmartDict(dict, llm Engine, cfg SmartDictConfig) *SmartDictEngine {
	return &SmartDictEngine{dict: dict, llm: llm, cfg: cfg}
}

// Name returns "smart-dict".
func (e *SmartDictEngine) Name() string { return "smart-dict" }

// Supports reports that this engine handles dictionary lookups.
func (e *SmartDictEngine) Supports(m Mode) bool { return m == ModeDict }

// Detect is not meaningful for a dictionary engine.
func (e *SmartDictEngine) Detect(ctx context.Context, text string) (string, error) {
	return "", nil
}

// Available reports true when either backend can serve (a dictionary hit, or the
// LLM fallback).
func (e *SmartDictEngine) Available(ctx context.Context) bool {
	return e.dict.Available(ctx) || e.llm.Available(ctx)
}

// Translate looks the word up in the dictionary and, on a miss or a too-weak fuzzy
// match, falls back to the LLM (streaming preserved).
func (e *SmartDictEngine) Translate(ctx context.Context, req Request) (<-chan Chunk, error) {
	dictCh, err := e.dict.Translate(ctx, req)
	if err != nil {
		// A setup failure (e.g. empty input) — nothing to fall back to.
		return nil, err
	}

	out := make(chan Chunk, 32)
	go func() {
		defer close(out)
		// The dictionary engines are single-shot (no token stream), so drain fully
		// before deciding whether the answer is good enough.
		res, derr := Drain(dictCh, nil)
		if e.dictAnswered(res, derr) {
			debug.Logf("smart-dict: %q answered from dictionary", strings.TrimSpace(req.Text))
			out <- Chunk{Kind: ChunkDone, Result: res}
			return
		}
		debug.Logf("smart-dict: %q → LLM fallback (no usable dictionary entry)", strings.TrimSpace(req.Text))
		e.fallback(ctx, req, derr, out)
	}()
	return out, nil
}

// dictAnswered reports whether the dictionary produced a result good enough to
// return without asking the LLM: an exact entry, or a fuzzy suggestion close enough
// to be a likely typo (within CloseDistance).
func (e *SmartDictEngine) dictAnswered(res *TranslateResult, derr error) bool {
	if derr != nil || res == nil {
		return false // hard miss (ErrNoDictEntry) or transport error → fall back
	}
	if res.Dictionary != nil {
		return true // exact hit
	}
	// A close typo (distance known and within threshold) stays as "did you mean".
	// Unknown distance (0, e.g. Chinese prefix matches) counts as too far → fall back.
	if len(res.Suggestions) > 0 && res.SuggestDistance != 0 && res.SuggestDistance <= e.cfg.CloseDistance {
		return true
	}
	return false // suggestions too far, "not installed" notes, or empty → fall back
}

// fallback runs the LLM in translate mode and pipes its chunks to out, stamping a
// warning on the terminal result so the downgrade is visible.
func (e *SmartDictEngine) fallback(ctx context.Context, req Request, derr error, out chan<- Chunk) {
	word := strings.TrimSpace(req.Text)

	r := req
	r.Mode = ModeTranslate
	r.Preset = e.cfg.Preset
	if r.Preset == "" {
		r.Preset = PresetDictionary
	}
	r.Target = smartTarget(req.Text, req.Target)

	llmCh, err := e.llm.Translate(ctx, r)
	if err != nil {
		out <- Chunk{Kind: ChunkError, Err: fmt.Errorf("smart-dict: no dictionary entry for %q and LLM fallback failed: %w", word, err)}
		return
	}
	for ch := range llmCh {
		if ch.Kind == ChunkDone && ch.Result != nil {
			ch.Result.Warnings = append(ch.Result.Warnings,
				fmt.Sprintf("no dictionary entry for %q — defined via %s (LLM)", word, ch.Result.Engine))
		}
		out <- ch
	}
}

// smartTarget picks the LLM fallback's target language, mirroring the bilingual
// dictionary (Chinese ↔ English): Chinese input → English; English/other input →
// the caller's target, or Chinese when that target is English/unset.
func smartTarget(text, target string) string {
	if lang.IsChinese(text) {
		return "en"
	}
	t := strings.TrimSpace(target)
	if t == "" || t == "auto" || strings.HasPrefix(strings.ToLower(t), "en") {
		return "zh"
	}
	return t
}
