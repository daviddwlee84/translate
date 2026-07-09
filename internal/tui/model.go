// Package tui implements the interactive Bubble Tea (v2, charm.land/*) front-end.
// It imports the shared engine layer but the engine layer never imports it.
package tui

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"translate/internal/engine"
)

// Params configures a TUI session.
type Params struct {
	Engine     engine.Engine
	Source     string
	Target     string
	Provider   string // display name for the footer
	Model      string // display model id for the footer
	Live       bool   // live-debounce default state
	DebounceMs int
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

	ta   textarea.Model
	vp   viewport.Model
	keys keyMap
	st   styles

	width, height int
	ready         bool

	source string
	target string
	live   bool

	// curEngine/curModel reflect the engine that actually served the last
	// result (may differ from p.Provider when the auto chain falls back).
	curEngine string
	curModel  string

	status status
	result *engine.TranslateResult
	err    error

	// request lifecycle: one monotonic seq drives debounce-collapse, cancel, and
	// stale-stream-drop. cancel/stream/streamBuf belong to the in-flight request.
	seq       int
	inflight  int
	cancel    context.CancelFunc
	stream    <-chan engine.Chunk
	streamBuf strings.Builder
	debounce  time.Duration
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

	return Model{
		p:        p,
		base:     ctx,
		ta:       ta,
		vp:       viewport.New(),
		keys:     defaultKeys(),
		st:       newStyles(),
		source:   p.Source,
		target:   p.Target,
		live:     p.Live,
		status:   statusIdle,
		debounce: debounce,
	}
}

// Init requests the initial window size and starts the cursor blink.
func (m Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, tea.RequestWindowSize)
}
