package tui

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"translate/internal/engine"
	"translate/internal/store"
)

// Update handles messages. Value receiver returning the modified copy is the
// single convention used throughout (see Model doc).
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.relayout()
		m.ready = true
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case debounceMsg:
		if msg.seq != m.seq {
			return m, nil // a newer keystroke arrived; this tick is stale
		}
		return m.launch()

	case historyLoadedMsg:
		m.hist.SetItems(toListItems(msg.items))
		return m, nil

	case modelsLoadedMsg:
		m.modelsLoading = false
		m.modelsErr = msg.err
		m.cachedModels = msg.models
		m.modelList.SetItems(modelItems(msg.models, m.currentModelID()))
		return m, nil

	case tea.MouseWheelMsg:
		return m.handleMouseWheel(msg)

	case tea.MouseClickMsg:
		return m.handleMouseClick(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.sp, cmd = m.sp.Update(msg)
		return m, cmd

	case streamMsg:
		return m.handleStream(msg)
	}

	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	return m, cmd
}

// handleStream applies one engine Chunk, dropping anything from a superseded
// request. This seq guard is the correctness mechanism: cancelled results can
// never render even if a goroutine emits one late.
func (m Model) handleStream(msg streamMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.inflight {
		return m, nil
	}
	switch msg.chunk.Kind {
	case engine.ChunkToken:
		m.streamBuf += msg.chunk.Text
		m.vp.SetContent(m.st.trans.Render(m.streamBuf))
		m.vp.GotoBottom()
		return m, waitStream(m.stream, m.inflight) // re-subscribe for the next token

	case engine.ChunkDone:
		m.inflight = 0
		m.status = statusDone
		m.err = nil
		if msg.chunk.Result != nil {
			m.result = msg.chunk.Result
			if msg.chunk.Result.Engine != "" {
				m.curEngine = msg.chunk.Result.Engine
			}
			if msg.chunk.Result.Model != "" {
				m.curModel = msg.chunk.Result.Model
			}
			m.vp.SetContent(m.renderResult(*msg.chunk.Result))
			return m, saveHistoryCmd(m.p.Store, m.recordFor(*msg.chunk.Result))
		}
		return m, nil

	case engine.ChunkError:
		m.inflight = 0
		m.status = statusError
		m.err = msg.chunk.Err
		m.vp.SetContent(m.st.errText.Render("✗ " + msg.chunk.Err.Error()))
		return m, nil
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// An open overlay (history / language / model picker) captures keys.
	if m.overlay != overlayNone {
		switch msg.String() {
		case "ctrl+c":
			m.cancelInflight()
			return m, tea.Quit
		case "esc":
			m.overlay = overlayNone
			return m, nil
		case "enter":
			return m.overlaySelect()
		}
		cmd := m.overlayListUpdate(msg)
		return m, cmd
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		m.cancelInflight()
		return m, tea.Quit

	case key.Matches(msg, m.keys.History):
		m.overlay = overlayHistory
		return m, loadHistoryCmd(m.p.Store)

	case key.Matches(msg, m.keys.PickLang):
		m.overlay = overlayLang
		return m, nil

	case key.Matches(msg, m.keys.PickModel):
		return m.openModelPicker()

	case key.Matches(msg, m.keys.ToggleLive):
		m.live = !m.live
		return m, nil

	case key.Matches(msg, m.keys.CycleEngine):
		if n := len(m.p.Engines); n > 1 {
			m.engIdx = (m.engIdx + 1) % n
			m.curEngine, m.curModel = "", ""
			if strings.TrimSpace(m.ta.Value()) != "" {
				return m.launch() // re-run with the newly selected engine
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.Clear):
		m.cancelInflight()
		m.seq++
		m.ta.Reset()
		m.result = nil
		m.err = nil
		m.streamBuf = ""
		m.status = statusIdle
		m.vp.SetContent("")
		return m, nil

	case key.Matches(msg, m.keys.Translate):
		return m.launch()
	}

	// Ordinary keystroke: update the textarea, then (re)arm the debounce.
	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)

	if strings.TrimSpace(m.ta.Value()) == "" {
		// Input cleared: cancel in-flight work and clear the result pane.
		m.cancelInflight()
		m.seq++
		m.result = nil
		m.streamBuf = ""
		m.status = statusIdle
		m.vp.SetContent("")
		return m, cmd
	}

	if m.live {
		m.seq++            // invalidate any pending debounce tick
		m.cancelInflight() // stop any in-flight request; its results become stale
		m.status = statusTyping
		return m, tea.Batch(cmd, armDebounce(m.seq, m.debounce))
	}
	return m, cmd
}

// launch starts a streaming translation for the current input, cancelling any
// in-flight request first. It is the single place a translation begins.
func (m Model) launch() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.ta.Value())
	if text == "" {
		m.status = statusIdle
		return m, nil
	}
	m.seq++
	if m.cancel != nil {
		m.cancel()
	}
	ctx, cancel := context.WithCancel(m.base)
	m.cancel = cancel
	m.inflight = m.seq
	m.status = statusTranslating
	m.streamBuf = ""
	m.err = nil
	m.result = nil
	m.lastInput = text

	seq := m.seq
	ne := m.active()
	req := engine.Request{
		Text:   text,
		Source: m.source,
		Target: m.target,
		Mode:   ne.Mode,
		Stream: true, // ignored by non-streaming engines (google/dict)
		Model:  m.modelOverride,
	}
	ch, err := ne.Engine.Translate(ctx, req)
	if err != nil {
		e := err
		return m, func() tea.Msg {
			return streamMsg{seq: seq, chunk: engine.Chunk{Kind: engine.ChunkError, Err: e}}
		}
	}
	m.stream = ch
	return m, waitStream(ch, seq)
}

// cancelInflight cancels the current request (if any) and marks it as no longer
// in flight so late chunks are dropped.
func (m *Model) cancelInflight() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.inflight = 0
}

// currentModelID is the model id currently in effect (for the ✓ marker).
func (m Model) currentModelID() string {
	if m.modelOverride != "" {
		return m.modelOverride
	}
	return m.curModel
}

// openModelPicker opens the model overlay, fetching the model list once per session.
func (m Model) openModelPicker() (tea.Model, tea.Cmd) {
	m.overlay = overlayModel
	if m.cachedModels == nil && !m.modelsLoading && m.modelsErr == nil {
		m.modelsLoading = true
		return m, fetchModelsCmd(m.p.ModelSource)
	}
	return m, nil
}

// overlayListUpdate forwards a message to the active overlay's list.
func (m *Model) overlayListUpdate(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch m.overlay {
	case overlayHistory:
		m.hist, cmd = m.hist.Update(msg)
	case overlayLang:
		m.langList, cmd = m.langList.Update(msg)
	case overlayModel:
		m.modelList, cmd = m.modelList.Update(msg)
	}
	return cmd
}

// overlaySelect applies the highlighted overlay item and closes the overlay.
func (m Model) overlaySelect() (tea.Model, tea.Cmd) {
	switch m.overlay {
	case overlayHistory:
		if it, ok := m.hist.SelectedItem().(histItem); ok {
			m.ta.SetValue(it.rec.Input)
			m.ta.MoveToEnd()
			m.overlay = overlayNone
			return m.launch()
		}
	case overlayLang:
		if it, ok := m.langList.SelectedItem().(langItem); ok {
			m.target = it.code
			m.overlay = overlayNone
			if strings.TrimSpace(m.ta.Value()) != "" {
				return m.launch()
			}
			return m, nil
		}
	case overlayModel:
		if it, ok := m.modelList.SelectedItem().(modelItem); ok {
			m.modelOverride = it.id
			m.overlay = overlayNone
			if strings.TrimSpace(m.ta.Value()) != "" {
				return m.launch()
			}
			return m, nil
		}
	}
	m.overlay = overlayNone
	return m, nil
}

// handleMouseWheel scrolls the active overlay list, or the result viewport.
func (m Model) handleMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if m.overlay != overlayNone {
		cmd := m.overlayListUpdate(msg)
		return m, cmd
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

// handleMouseClick routes clicks. In an overlay it forwards to the list; in
// normal mode a click on the footer row opens the language picker (like clicking
// the language selector in a translator UI).
func (m Model) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if m.overlay != overlayNone {
		cmd := m.overlayListUpdate(msg)
		return m, cmd
	}
	if m.height > 0 && msg.Y == m.height-1 {
		m.overlay = overlayLang
		return m, nil
	}
	return m, nil
}

// recordFor builds a history record for a completed result.
func (m Model) recordFor(res engine.TranslateResult) store.Record {
	src := m.source
	if src == "auto" && res.DetectedSource != "" {
		src = res.DetectedSource
	}
	return store.Record{
		SourceLang:   src,
		TargetLang:   m.target,
		Engine:       res.Engine,
		Model:        res.Model,
		Input:        m.lastInput,
		Output:       res.Translation,
		Alternatives: res.Alternatives,
		Notes:        res.Notes,
	}
}

// relayout recomputes component sizes for the current window. Widths account for
// each box's border (2) and horizontal padding (2).
func (m *Model) relayout() {
	if m.width == 0 || m.height == 0 {
		return
	}
	const (
		footerH = 1
		inputH  = 4
	)
	compW := m.width - 4
	if compW < 1 {
		compW = 1
	}
	resultH := m.height - footerH - (inputH + 2) - 2
	if resultH < 3 {
		resultH = 3
	}
	m.ta.SetWidth(compW)
	m.ta.SetHeight(inputH)
	m.vp.SetWidth(compW)
	m.vp.SetHeight(resultH)
	overlayH := m.height - footerH - 1
	m.hist.SetSize(m.width-2, overlayH)
	m.langList.SetSize(m.width-2, overlayH)
	m.modelList.SetSize(m.width-2, overlayH)
}
