package engine

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/daviddwlee84/translate/internal/lang"
)

// LocalDictConfig configures the offline bilingual dictionary engine.
type LocalDictConfig struct {
	Dir          string
	CedictURL    string
	EcdictURL    string
	AutoDownload bool        // auto-fetch CC-CEDICT (small) on first Chinese lookup
	Fuzzy        bool        // emit ranked suggestions on a miss
	APIFallback  *DictEngine // nil unless configured; used for English when ECDICT is absent
	Timeout      time.Duration
}

// LocalDictEngine looks up definitions offline: Chinese → CC-CEDICT (zh→en),
// English → ECDICT (en→zh). It satisfies the Engine interface and maps results
// into the same DictEntry/Suggestions shape the TUI already renders.
type LocalDictEngine struct {
	cfg LocalDictConfig
	ce  *cedictIndex
	ec  *ecdictDB
	wl  *wordIndex // /usr/share/dict/words, for English "did you mean"
}

// NewLocalDict builds a local bilingual dictionary engine.
func NewLocalDict(cfg LocalDictConfig) *LocalDictEngine {
	return &LocalDictEngine{
		cfg: cfg,
		ce:  newCedictIndex(CedictPath(cfg.Dir)),
		ec:  newEcdictDB(EcdictDBPath(cfg.Dir)),
		wl:  &wordIndex{path: "/usr/share/dict/words"},
	}
}

func (e *LocalDictEngine) Name() string         { return "dictionary" }
func (e *LocalDictEngine) Supports(m Mode) bool { return m == ModeDict }

func (e *LocalDictEngine) Detect(ctx context.Context, text string) (string, error) {
	return "", nil
}

// Available reports true if at least one data source (or the API fallback) is usable.
func (e *LocalDictEngine) Available(ctx context.Context) bool {
	if fileExists(CedictPath(e.cfg.Dir)) || e.ec.available() {
		return true
	}
	if e.cfg.APIFallback != nil {
		return e.cfg.APIFallback.Available(ctx)
	}
	return false
}

// Translate routes by script: Chinese → CC-CEDICT, else → ECDICT.
func (e *LocalDictEngine) Translate(ctx context.Context, req Request) (<-chan Chunk, error) {
	word := strings.TrimSpace(req.Text)
	if word == "" {
		return nil, ErrEmptyInput
	}
	if lang.IsChinese(word) {
		return e.lookupZh(ctx, word, req), nil
	}
	return e.lookupEn(ctx, word, req), nil
}

func (e *LocalDictEngine) lookupZh(ctx context.Context, word string, req Request) <-chan Chunk {
	if !fileExists(CedictPath(e.cfg.Dir)) {
		if e.cfg.AutoDownload {
			if err := DownloadCedict(ctx, e.cfg.CedictURL, CedictPath(e.cfg.Dir), nil); err != nil {
				return single(nil, fmt.Errorf("dictionary: %w", err))
			}
		} else {
			return single(notInstalled(req, "CC-CEDICT not installed — run `translate dict update cedict`"), nil)
		}
	}
	if entries := e.ce.lookup(word); len(entries) > 0 {
		return single(cedictResult(word, entries, req), nil)
	}
	if e.cfg.Fuzzy {
		if sugg := e.ce.prefixSuggest(word, suggestLimit); len(sugg) > 0 {
			return single(suggestResult(sugg, req), nil)
		}
	}
	return single(nil, fmt.Errorf("dictionary: %w: %q", ErrNoDictEntry, word))
}

func (e *LocalDictEngine) lookupEn(ctx context.Context, word string, req Request) <-chan Chunk {
	if !e.ec.available() {
		// No ECDICT yet: fall back to dictionaryapi.dev (English defs) if enabled.
		if e.cfg.APIFallback != nil {
			ch, err := e.cfg.APIFallback.Translate(ctx, req)
			if err == nil {
				return ch
			}
		}
		return single(notInstalled(req, "ECDICT not installed — run `translate dict update ecdict`"), nil)
	}
	en, err := e.ec.lookup(ctx, word)
	if err != nil {
		return single(nil, fmt.Errorf("dictionary: %w", err))
	}
	if en != nil {
		return single(ecdictResult(en, req), nil)
	}
	if e.cfg.Fuzzy {
		if sugg := e.wl.nearestN(strings.ToLower(word), 2, suggestLimit); len(sugg) > 0 {
			return single(suggestResult(sugg, req), nil)
		}
	}
	return single(nil, fmt.Errorf("dictionary: %w: %q", ErrNoDictEntry, word))
}

// --- result mapping into the existing DictEntry / Suggestions shape ---

func cedictResult(word string, entries []*cedictEntry, req Request) *TranslateResult {
	d := &DictEntry{Word: word}
	if len(entries) > 0 {
		d.Phonetic = entries[0].Pinyin
	}
	for _, en := range entries {
		mn := Meaning{PartOfSpeech: en.Pinyin}
		for _, def := range en.Defs {
			mn.Definitions = append(mn.Definitions, Definition{Text: def})
		}
		d.Meanings = append(d.Meanings, mn)
	}
	gloss := ""
	if len(entries) > 0 && len(entries[0].Defs) > 0 {
		gloss = entries[0].Defs[0]
	}
	return &TranslateResult{Translation: gloss, Target: req.Target, Engine: "dictionary", Dictionary: d}
}

func ecdictResult(en *ecdictEntry, req Request) *TranslateResult {
	d := &DictEntry{Word: en.Word, Phonetic: en.Phonetic}
	if zh := splitEcdict(en.Translation); len(zh) > 0 {
		mn := Meaning{PartOfSpeech: "中文"}
		for _, line := range zh {
			mn.Definitions = append(mn.Definitions, Definition{Text: line})
		}
		d.Meanings = append(d.Meanings, mn)
	}
	if de := splitEcdict(en.Definition); len(de) > 0 {
		mn := Meaning{PartOfSpeech: "definition"}
		for _, line := range de {
			mn.Definitions = append(mn.Definitions, Definition{Text: line})
		}
		d.Meanings = append(d.Meanings, mn)
	}
	gloss := ""
	if zh := splitEcdict(en.Translation); len(zh) > 0 {
		gloss = zh[0]
	}
	return &TranslateResult{Translation: gloss, Target: req.Target, Engine: "dictionary", Dictionary: d}
}

func suggestResult(sugg []string, req Request) *TranslateResult {
	return &TranslateResult{Target: req.Target, Engine: "dictionary", Suggestions: sugg}
}

func notInstalled(req Request, note string) *TranslateResult {
	return &TranslateResult{Target: req.Target, Engine: "dictionary", Notes: note}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
