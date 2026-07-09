package tui

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/key"
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
		m.streamBuf.WriteString(msg.chunk.Text)
		m.vp.SetContent(m.st.trans.Render(m.streamBuf.String()))
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
	// History browsing mode captures keys until closed.
	if m.showHistory {
		switch msg.String() {
		case "ctrl+c":
			m.cancelInflight()
			return m, tea.Quit
		case "esc", "ctrl+r":
			m.showHistory = false
			return m, nil
		case "enter":
			if it, ok := m.hist.SelectedItem().(histItem); ok {
				m.ta.SetValue(it.rec.Input)
				m.ta.MoveToEnd()
				m.showHistory = false
				return m.launch()
			}
			m.showHistory = false
			return m, nil
		}
		var cmd tea.Cmd
		m.hist, cmd = m.hist.Update(msg)
		return m, cmd
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		m.cancelInflight()
		return m, tea.Quit

	case key.Matches(msg, m.keys.History):
		m.showHistory = true
		return m, loadHistoryCmd(m.p.Store)

	case key.Matches(msg, m.keys.ToggleLive):
		m.live = !m.live
		return m, nil

	case key.Matches(msg, m.keys.Clear):
		m.cancelInflight()
		m.seq++
		m.ta.Reset()
		m.result = nil
		m.err = nil
		m.streamBuf.Reset()
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
		m.streamBuf.Reset()
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
	m.streamBuf.Reset()
	m.err = nil
	m.result = nil
	m.lastInput = text

	seq := m.seq
	req := engine.Request{
		Text:   text,
		Source: m.source,
		Target: m.target,
		Mode:   engine.ModeTranslate,
		Stream: true,
	}
	ch, err := m.p.Engine.Translate(ctx, req)
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
	m.hist.SetSize(m.width-2, m.height-footerH-1)
}
