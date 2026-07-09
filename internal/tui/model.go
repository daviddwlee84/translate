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

	"translate/internal/engine"
	"translate/internal/store"
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
	Model         string // display model id for the footer (initial)
	Live          bool   // live-debounce default state
	DebounceMs    int
}

type status int

const (
	statusIdle status = iota
	statusTyping
	statusTranslating
	statusDone
	statusError
)

// Model is the Bubble Tea model. All methods use value receivers and return the
// modified copy (the v2 convention) to avoid "state didn't stick" bugs.
type Model struct {
	p    Params
	base context.Context

	ta        textarea.Model
	vp        viewport.Model
	hist      list.Model
	langList  list.Model
	modelList list.Model
	sp        spinner.Model
	keys      keyMap
	st        styles

	width, height int
	ready         bool
	overlay       overlayKind
	resultH       int // result viewport height (kept fixed to avoid layout jumps)

	source string
	target string
	live   bool
	engIdx int // index into p.Engines (the active engine)

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

	// request lifecycle: one monotonic seq drives debounce-collapse, cancel, and
	// stale-stream-drop. cancel/stream/streamBuf belong to the in-flight request.
	// streamBuf is a plain string (NOT strings.Builder): the model is copied by
	// value on every Update, and copying a used Builder panics.
	seq       int
	inflight  int
	cancel    context.CancelFunc
	stream    <-chan engine.Chunk
	streamBuf string
	debounce  time.Duration
	lastInput string // input text that produced the in-flight/last request
	lastDone  string // input text of the last COMPLETED translation (skip re-runs)
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

	sp := spinner.New()
	sp.Spinner = spinner.MiniDot

	return Model{
		p:         p,
		base:      ctx,
		ta:        ta,
		vp:        viewport.New(),
		hist:      hist,
		langList:  langList,
		modelList: modelList,
		sp:        sp,
		keys:      defaultKeys(),
		st:        newStyles(),
		source:    p.Source,
		target:    p.Target,
		live:      p.Live,
		status:    statusIdle,
		debounce:  debounce,
	}
}

// Pair returns the current source and target language codes (read by the caller
// after the program exits to persist the last pair).
func (m Model) Pair() (source, target string) { return m.source, m.target }

// Init requests the initial window size and starts the cursor blink + spinner.
func (m Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.sp.Tick, tea.RequestWindowSize)
}
