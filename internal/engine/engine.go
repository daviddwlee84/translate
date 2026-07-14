// Package engine defines the translation backend abstraction and its
// implementations (LLM via OpenAI-compatible APIs, free web APIs, dictionaries).
//
// The engine layer is pure Go with context.Context and imports zero TUI code:
// the one-shot CLI and the Bubble Tea TUI drive the identical Engine values and
// diverge only at presentation.
package engine

import "context"

// Mode selects what kind of lookup a Request performs.
type Mode int

const (
	// ModeTranslate is register-aware, faithful translation (the default).
	ModeTranslate Mode = iota
	// ModeDict is a dictionary/definition lookup.
	ModeDict
)

// Request is the single input to every engine. A Source of "" or "auto" asks
// the engine to detect the source language.
type Request struct {
	Text    string
	Source  string // BCP-47-ish code or "auto"/""
	Target  string // e.g. "en", "zh"
	Mode    Mode
	MaxAlts int    // cap on Alternatives; 0 => engine default
	Stream  bool   // caller wants token streaming (LLM only; others ignore)
	Model   string // optional per-request model override (LLM engines)
	// ModelProvider scopes Model to one provider by name; other engines ignore
	// the override (so a copilot model id never leaks to an Ollama fallback).
	ModelProvider string
	Preset        string // LLM prompt style: "" => concise (LLM translate only)
	Extra         string // extra user instructions appended to the system prompt

	// Pair marks a bidirectional "pair" request: the text is written in one of
	// two languages (PairHome/PairAway) and must be translated into the OTHER.
	// The LLM translate prompt uses this to detect-and-route and, critically, to
	// never echo the input unchanged. Target still carries the caller's routed
	// best guess (for history/display and non-LLM engines).
	Pair     bool
	PairHome string // one pair language (e.g. the home target, "zh-TW")
	PairAway string // the other pair language (e.g. "en")

	// Learn marks a language-learning request. It is bidirectional (implies Pair,
	// with PairHome=native and PairAway=foreign) and adaptive: native-language input
	// is translated-and-taught (glosses + examples), foreign-language input is
	// grammar-corrected-and-explained. The LLM engine answers with a strict JSON
	// object (parsed into TranslateResult.Learn) and, because the output is
	// structured, always runs NON-STREAMING regardless of Stream.
	Learn bool
}

// TranslateResult is the "Marvin-lite" typed result. Every engine fills what it
// can; zero-value fields are simply absent. It is also the history record shape.
type TranslateResult struct {
	Translation    string   `json:"translation"`
	DetectedSource string   `json:"detected_source,omitempty"`
	Target         string   `json:"target"`
	Alternatives   []string `json:"alternatives,omitempty"`
	Notes          string   `json:"notes,omitempty"`
	Confidence     float64  `json:"confidence,omitempty"`

	// Warnings records non-fatal problems, e.g. a chain engine that failed before
	// this (fallback) engine served the result — so a downgrade is never silent.
	Warnings []string `json:"warnings,omitempty"`

	// Truncated is set when a streamed result ended before the model finished
	// (no terminal marker, or stop_reason=="max_tokens"). The partial text is
	// still returned so the caller can show it, but it must not be cached or
	// persisted as if complete. Transient: not serialized.
	Truncated bool `json:"-"`

	// Provenance — stamped by the producing engine / chain.
	Engine string `json:"engine,omitempty"`
	Model  string `json:"model,omitempty"`

	// Dictionary payload (Mode == ModeDict); nil for translate mode.
	Dictionary *DictEntry `json:"dictionary,omitempty"`
	// Learn payload (Request.Learn); nil outside learn mode. Translation still holds
	// the main foreign-language sentence (the corrected sentence, or the translation)
	// so copy/history/speak keep working unchanged; the structured extras ride here.
	Learn *LearnResult `json:"learn,omitempty"`
	// Suggestions is a ranked "did you mean" list, set by the dictionary engine
	// when no exact entry was found (Dictionary and Translation stay empty). The
	// frontend decides how to present/select — the engine no longer auto-picks.
	Suggestions []string `json:"suggestions,omitempty"`

	// SuggestDistance is the edit distance of the best entry in Suggestions (English
	// fuzzy lookups only; 0 when unknown/not applicable, e.g. Chinese prefix matches).
	// The smart-dict engine reads it to tell a likely typo from a match too far off.
	// Transient: not serialized.
	SuggestDistance int `json:"-"`
}

// DictEntry is a dictionary lookup payload (populated by the dictionary engine).
type DictEntry struct {
	Word      string    `json:"word"`
	Phonetic  string    `json:"phonetic,omitempty"`
	Meanings  []Meaning `json:"meanings,omitempty"`
	SourceURL string    `json:"source_url,omitempty"`
}

// Meaning groups definitions by part of speech.
type Meaning struct {
	PartOfSpeech string       `json:"part_of_speech"`
	Definitions  []Definition `json:"definitions"`
	Synonyms     []string     `json:"synonyms,omitempty"`
	Antonyms     []string     `json:"antonyms,omitempty"`
}

// Definition is a single sense of a word.
type Definition struct {
	Text    string `json:"definition"`
	Example string `json:"example,omitempty"`
}

// LearnResult is the structured payload for learn/tutor mode (Request.Learn). The
// model fills only the fields relevant to the auto-detected direction; all glosses,
// explanations, and notes are written in the native (home) language.
type LearnResult struct {
	Direction   string         `json:"direction"`           // "teach" (native→foreign) | "correct" (foreign→native)
	Original    string         `json:"original"`            // the user's input as received
	Corrected   string         `json:"corrected,omitempty"` // correct-direction: the fixed foreign sentence
	Translation string         `json:"translation"`         // teach: foreign translation; correct: native translation of intent
	Notes       string         `json:"notes,omitempty"`     // short tip/encouragement, in the NATIVE language
	Issues      []LearnIssue   `json:"issues,omitempty"`    // correct-direction grammar/usage fixes
	Vocab       []LearnGloss   `json:"vocab,omitempty"`     // teach-direction glosses
	Examples    []LearnExample `json:"examples,omitempty"`  // teach-direction usage examples
}

// LearnIssue is one grammar/usage correction (correct-direction).
type LearnIssue struct {
	Span        string `json:"span"`        // the problematic fragment of the input
	Fix         string `json:"fix"`         // the corrected fragment
	Explanation string `json:"explanation"` // why — in the NATIVE language
}

// LearnGloss is one vocabulary explanation (teach-direction).
type LearnGloss struct {
	Term     string `json:"term"`               // foreign word/term
	Pos      string `json:"pos,omitempty"`      // part of speech
	Phonetic string `json:"phonetic,omitempty"` // KK/IPA for English, pinyin for Chinese
	Meaning  string `json:"meaning"`            // meaning in the NATIVE language
}

// LearnExample is one usage example (teach-direction).
type LearnExample struct {
	Foreign string `json:"foreign"` // example sentence in the foreign language
	Native  string `json:"native"`  // its native-language translation
}

// ChunkKind distinguishes streamed tokens from the terminal result/error.
type ChunkKind int

const (
	// ChunkToken is one streamed unit of translation text.
	ChunkToken ChunkKind = iota
	// ChunkDone carries the final structured result and ends the stream.
	ChunkDone
	// ChunkError carries a terminal error and ends the stream.
	ChunkError
)

// Chunk is one unit emitted on an engine's result channel.
type Chunk struct {
	Kind   ChunkKind
	Text   string           // token text (ChunkToken)
	Result *TranslateResult // final result (ChunkDone)
	Err    error            // terminal error (ChunkError)
}

// Engine is the provider abstraction. Every method honors ctx cancellation.
//
// Translate returns a receive-only channel and MUST close it after sending
// exactly one terminal Chunk (ChunkDone or ChunkError). Streaming engines emit
// zero or more ChunkToken then one ChunkDone; non-streaming engines emit a
// single ChunkDone. This uniform "channel closed == finished" contract lets the
// TUI's self-resubscribing reader and the CLI's drain loop share one code path.
// The synchronous error return is only for immediate setup failures; all
// runtime/network errors flow as a terminal ChunkError.
type Engine interface {
	Name() string
	Translate(ctx context.Context, req Request) (<-chan Chunk, error)
	Detect(ctx context.Context, text string) (string, error)
	Available(ctx context.Context) bool
	Supports(m Mode) bool
}

// single is a helper for non-streaming engines: it returns a closed channel
// carrying exactly one terminal Chunk.
func single(res *TranslateResult, err error) <-chan Chunk {
	ch := make(chan Chunk, 1)
	if err != nil {
		ch <- Chunk{Kind: ChunkError, Err: err}
	} else {
		ch <- Chunk{Kind: ChunkDone, Result: res}
	}
	close(ch)
	return ch
}

// Drain consumes an engine channel to completion, returning the final result.
// It streams tokens to onToken (if non-nil) as they arrive. Used by the
// one-shot CLI path; the TUI drives the channel directly via its Update loop.
func Drain(ch <-chan Chunk, onToken func(string)) (*TranslateResult, error) {
	for c := range ch {
		switch c.Kind {
		case ChunkToken:
			if onToken != nil {
				onToken(c.Text)
			}
		case ChunkDone:
			return c.Result, nil
		case ChunkError:
			return nil, c.Err
		}
	}
	return nil, ErrNoResult
}
