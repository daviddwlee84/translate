// Package tui implements the interactive Bubble Tea (v2, charm.land/*) front-end.
// It imports the shared engine layer but the engine layer never imports it.
package tui

import (
	"context"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/daviddwlee84/translate/internal/engine"
	"github.com/daviddwlee84/translate/internal/store"
	"github.com/daviddwlee84/translate/internal/tts"
)

// NamedEngine is one selectable engine in the TUI's ^e cycle.
type NamedEngine struct {
	Name   string // "auto", "google", "dictionary", …
	Engine engine.Engine
	Mode   engine.Mode
}

// Params configures a TUI session.
type Params struct {
	Engines       []NamedEngine      // selectable engines; index 0 is the default
	ModelSource   engine.ModelLister // fetches the model list for the model picker (may be nil)
	ModelProvider string             // provider name the model override applies to
	Store         store.Store        // nil when history is disabled
	Source        string
	Target        string
	Pair          bool          // bidirectional pair mode
	PairWith      string        // the "away" language (resolved code)
	Learn         bool          // start with learning mode (^n) on
	LearnEngine   engine.Engine // bare LLM engine for learn requests (nil => learn disabled)
	Model         string        // display model id for the footer (initial)
	Preset        string        // LLM prompt style
	Instructions  string        // extra system-prompt guidance
	Live          bool          // live-debounce default state
	DebounceMs    int
	Speaker       tts.Speaker // TTS backend for ^s (nil when disabled)
	Foreign       string      // preferred "副"/foreign language to speak ("" => derive)
}

type status int

const (
	statusIdle status = iota
	statusTyping
	statusTranslating
	statusDone
	statusError
)

// focusPane selects which pane keyboard navigation drives. Global bindings
// (Enter, ^y, ^e, …) work regardless; only ordinary/scroll keys are routed.
type focusPane int

const (
	focusInput  focusPane = iota // typing + cursor navigation in the textarea (default)
	focusOutput                  // keyboard scrolling of the result viewport
)

// pairMode is the bidirectional-pair direction control, cycled by ^g. Beyond
// on/off it can PIN the output language (bypassing auto-detection), so the user
// can force an ad-hoc direction without leaving their pair setup.
type pairMode int

const (
	pairOff  pairMode = iota // not a pair: plain source→target (manual, any ^t target)
	pairAuto                 // detect the input's language and translate to the OTHER side
	pairAway                 // force output → the away language (pairWith)
	pairHome                 // force output → the home language (target)
)

// nextPairMode advances the ^g cycle: auto → →away → →home → off → auto.
func nextPairMode(p pairMode) pairMode {
	switch p {
	case pairOff:
		return pairAuto
	case pairAuto:
		return pairAway
	case pairAway:
		return pairHome
	default: // pairHome
		return pairOff
	}
}

// initialPairMode seeds the mode from the launch --pair flag.
func initialPairMode(on bool) pairMode {
	if on {
		return pairAuto
	}
	return pairOff
}

// inputH is the fixed height (rows) of the input textarea box; shared by the
// layout and the click-to-focus hit test.
const inputH = 4

// Model is the Bubble Tea model. All methods use value receivers and return the
// modified copy (the v2 convention) to avoid "state didn't stick" bugs.
type Model struct {
	p    Params
	base context.Context

	ta          textarea.Model
	vp          viewport.Model
	hist        list.Model
	langList    list.Model
	modelList   list.Model
	suggestList list.Model
	presetList  list.Model
	sp          spinner.Model
	keys        keyMap
	st          styles

	width, height int
	ready         bool
	overlay       overlayKind
	focus         focusPane // input (default) vs output; drives scroll/typing routing
	resultH       int       // result viewport height (kept fixed to avoid layout jumps)

	source   string
	target   string
	pairMode pairMode
	pairWith string
	learn    bool // learning mode (^n): structured teach/correct via p.LearnEngine
	preset   string
	live     bool
	engIdx   int // index into p.Engines (the active engine)

	// model picker state (session-cached)
	cachedModels  []string
	modelOverride string
	modelsLoading bool
	modelsErr     error

	// curEngine/curModel reflect the engine that actually served the last
	// result (may differ from the selected engine when the auto chain falls back).
	curEngine string
	curModel  string

	status status
	result *engine.TranslateResult
	err    error
	flash  string // transient footer notice (e.g. "copied ✓")

	// speakCancel cancels in-flight TTS playback (^s); a new ^s or Clear/Quit
	// cancels the previous one so playback never overlaps.
	speakCancel context.CancelFunc

	// request lifecycle: one monotonic seq drives debounce-collapse, cancel, and
	// stale-stream-drop. cancel/stream/streamBuf belong to the in-flight request.
	// streamBuf is a plain string (NOT strings.Builder): the model is copied by
	// value on every Update, and copying a used Builder panics.
	seq         int
	inflight    int
	cancel      context.CancelFunc
	stream      <-chan engine.Chunk
	streamBuf   string
	debounce    time.Duration
	lastInput   string     // input text that produced the in-flight/last request
	lastDoneKey cacheKey   // key of the last COMPLETED result (skip identical re-runs)
	pendingKey  cacheKey   // cache key of the in-flight request
	cache       cacheStore // session result cache (live cache-hit = instant, no API call)

	// Truncation auto-retry: a truncated stream (rare copilot-proxy drop) is
	// re-fired once automatically, since a fresh request almost always completes.
	// autoRetryKey is the pendingKey we've already retried once (so we retry at
	// most once per input); retrying drives the "retrying" placeholder label.
	autoRetryKey cacheKey
	retrying     bool
}

// active returns the currently selected engine.
func (m Model) active() NamedEngine {
	if len(m.p.Engines) == 0 {
		return NamedEngine{}
	}
	return m.p.Engines[m.engIdx%len(m.p.Engines)]
}

// New builds a TUI model.
func New(ctx context.Context, p Params) Model {
	ta := textarea.New()
	ta.Placeholder = "Type text to translate…"
	ta.ShowLineNumbers = false
	ta.SetVirtualCursor(true)
	// Enter triggers translation, so move newline insertion to Alt+Enter.
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter"))
	ta.Focus()

	debounce := time.Duration(p.DebounceMs) * time.Millisecond
	if debounce <= 0 {
		debounce = 400 * time.Millisecond
	}

	hist := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	hist.Title = "History (↵ recall · esc close)"
	hist.SetShowHelp(false)

	langList := list.New(langItems(), list.NewDefaultDelegate(), 0, 0)
	langList.Title = "Target language (↵ select · esc close)"
	langList.SetShowHelp(false)

	modelList := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	modelList.Title = "Model (↵ select · esc close)"
	modelList.SetShowHelp(false)

	suggestList := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	suggestList.Title = "Did you mean? (↵ look up · esc close)"
	suggestList.SetShowHelp(false)

	preset := p.Preset
	if preset == "" {
		preset = engine.PresetConcise
	}
	presetList := list.New(presetItems(preset), list.NewDefaultDelegate(), 0, 0)
	presetList.Title = "Style (↵ select · esc close)"
	presetList.SetShowHelp(false)

	sp := spinner.New()
	sp.Spinner = spinner.MiniDot

	// Soft-wrap long paragraph lines to the viewport width; without this a single
	// long (unwrapped) translation line is clipped and its tail is lost on screen.
	vp := viewport.New()
	vp.SoftWrap = true

	return Model{
		p:           p,
		base:        ctx,
		ta:          ta,
		vp:          vp,
		hist:        hist,
		langList:    langList,
		modelList:   modelList,
		suggestList: suggestList,
		presetList:  presetList,
		sp:          sp,
		keys:        defaultKeys(),
		st:          newStyles(),
		source:      p.Source,
		target:      p.Target,
		pairMode:    initialPairMode(p.Pair),
		pairWith:    p.PairWith,
		learn:       p.Learn && p.LearnEngine != nil,
		preset:      preset,
		live:        p.Live,
		status:      statusIdle,
		debounce:    debounce,
		cache:       cacheStore{},
	}
}

// Pair returns the current source and target language codes (read by the caller
// after the program exits to persist the last pair).
func (m Model) Pair() (source, target string) { return m.source, m.target }

// pairOn reports whether any bidirectional-pair mode is active (auto or forced).
func (m Model) pairOn() bool { return m.pairMode != pairOff }

// Init requests the initial window size and starts the cursor blink + spinner.
func (m Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.sp.Tick, tea.RequestWindowSize)
}
