package tui

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"

	"github.com/daviddwlee84/translate/internal/engine"
	"github.com/daviddwlee84/translate/internal/lang"
	"github.com/daviddwlee84/translate/internal/store"
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
		return m.launch(false)

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
		// Animate the placeholder inside the (fixed-height) viewport so the
		// layout never jumps while waiting for the first token.
		if m.status == statusTranslating && m.streamBuf == "" {
			m.vp.SetContent(m.translatingPlaceholder())
		}
		return m, cmd

	case flashClearMsg:
		m.flash = ""
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
		m.retrying = false // real tokens are flowing; drop the "retrying" label
		m.streamBuf += msg.chunk.Text
		m.vp.SetContent(m.st.trans.Render(m.streamBuf))
		m.vp.GotoBottom()
		return m, waitStream(m.stream, m.inflight) // re-subscribe for the next token

	case engine.ChunkDone:
		m.inflight = 0
		m.status = statusDone
		m.err = nil
		m.lastDoneKey = m.pendingKey // remember what was translated (skip re-runs)
		if msg.chunk.Result != nil {
			r := *msg.chunk.Result
			// A truncated stream (rare copilot-proxy drop) is re-fired once
			// automatically — a fresh request almost always completes. Only after a
			// second truncation do we settle on the partial text + ⚠.
			if r.Truncated && m.autoRetryKey != m.pendingKey {
				m.autoRetryKey = m.pendingKey
				nm, cmd := m.launch(true) // force a fresh fetch of the same input
				mm := nm.(Model)
				mm.retrying = true
				mm.vp.SetContent(mm.translatingPlaceholder())
				return mm, cmd
			}
			m.autoRetryKey = cacheKey{} // settled (complete, or gave up after the retry)
			m.retrying = false
			m.result = msg.chunk.Result
			// A truncated result keeps its partial text on screen (with a ⚠), but
			// must not be cached or persisted as if it were the real answer — so a
			// manual retry (Enter) actually re-fetches instead of replaying it.
			if !r.Truncated {
				m.cache[m.pendingKey] = msg.chunk.Result // write-through (force overwrites)
			}
			if r.Engine != "" {
				m.curEngine = r.Engine
			}
			if r.Model != "" {
				m.curModel = r.Model
			}
			m.relayout() // engine/model segment width may have changed
			m.vp.SetContent(m.renderResult(r))
			m.vp.GotoTop() // show a completed result from its start, not scrolled
			// Don't persist a truncated result, or a suggestions-only result
			// (empty translation, no entry).
			if !r.Truncated && (r.Dictionary != nil || r.Translation != "") {
				return m, saveHistoryCmd(m.p.Store, m.recordFor(r))
			}
			return m, nil
		}
		return m, nil

	case engine.ChunkError:
		m.inflight = 0
		m.retrying = false
		m.autoRetryKey = cacheKey{}
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
		if m.active().Mode == engine.ModeDict {
			return m, nil // no model concept in dictionary mode
		}
		return m.openModelPicker()

	case key.Matches(msg, m.keys.PickPreset):
		if m.active().Mode == engine.ModeDict {
			return m, nil // presets are LLM translate styles
		}
		m.presetList.SetItems(presetItems(m.preset))
		m.overlay = overlayPreset
		return m, nil

	case key.Matches(msg, m.keys.TogglePair):
		m.pair = !m.pair
		if m.pair && m.pairWith == "" {
			m.pairWith = "en"
		}
		m.relayout() // footer pair segment changed
		if m.live && strings.TrimSpace(m.ta.Value()) != "" {
			return m.launch(false)
		}
		m.clearResult()
		return m, nil

	case key.Matches(msg, m.keys.Copy):
		if txt := m.copyText(); txt != "" {
			if err := clipboard.WriteAll(txt); err != nil {
				m.flash = "copy failed"
			} else {
				m.flash = "copied ✓"
			}
			return m, flashCmd()
		}
		return m, nil

	case key.Matches(msg, m.keys.ToggleLive):
		m.live = !m.live
		return m, nil

	case key.Matches(msg, m.keys.CycleEngine):
		if n := len(m.p.Engines); n > 1 {
			m.engIdx = (m.engIdx + 1) % n
			m.curEngine, m.curModel = "", ""
			m.relayout() // footer content (engine/mode) changed
			// Only re-translate automatically in live mode; otherwise switch the
			// engine, clear the now-stale result, and wait for Enter.
			if m.live && strings.TrimSpace(m.ta.Value()) != "" {
				return m.launch(true)
			}
			m.clearResult()
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
		// After a dictionary miss the input still equals the missed word, so a
		// second Enter means "help me choose" — open the ranked suggestions.
		if m.result != nil && len(m.result.Suggestions) > 0 &&
			strings.TrimSpace(m.ta.Value()) == m.lastInput {
			m.suggestList.SetItems(suggestItems(m.result.Suggestions))
			m.overlay = overlaySuggest
			return m, nil
		}
		return m.launch(true)
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

// translatingPlaceholder renders the fixed-height "translating…" placeholder,
// upgraded to a "retrying" label while an auto-retry after a truncation is
// in flight so the user sees why a second request fired.
func (m Model) translatingPlaceholder() string {
	label := "translating…"
	if m.retrying {
		label = "translating… · retrying (stream truncated)"
	}
	return m.sp.View() + " " + m.st.dim.Render(label)
}

// launch starts a streaming translation for the current input, cancelling any
// in-flight request first. It is the single place a translation begins.
// When force is false (the live/debounce path) it skips re-translating text that
// was already translated, to avoid redundant LLM/API calls.
func (m Model) launch(force bool) (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.ta.Value())
	if text == "" {
		m.status = statusIdle
		return m, nil
	}
	key := m.cacheKeyFor(text)
	if !force && key == m.lastDoneKey && m.status == statusDone {
		return m, nil // already showing this exact result
	}
	// Cache hit on the live/debounce path: render instantly with NO API call.
	// Enter (force) always re-fetches and overwrites.
	if !force {
		if cached, ok := m.cache[key]; ok {
			m.cancelInflight()
			m.status = statusDone
			m.err = nil
			m.result = cached
			if cached.Engine != "" {
				m.curEngine = cached.Engine
			}
			if cached.Model != "" {
				m.curModel = cached.Model
			}
			m.streamBuf = ""
			m.lastInput = text
			m.lastDoneKey = key
			m.vp.SetContent(m.renderResult(*cached))
			return m, nil
		}
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
	m.retrying = false // a fresh launch clears any stale "retrying" label
	m.err = nil
	m.result = nil
	m.lastInput = text
	m.pendingKey = key
	m.vp.SetContent(m.translatingPlaceholder()) // fixed-height placeholder

	seq := m.seq
	ne := m.active()
	target := m.target
	if m.pair && m.pairWith != "" && ne.Mode == engine.ModeTranslate {
		target = lang.PairTarget(m.target, m.pairWith, text) // home-lang input → away, else home
	}
	req := engine.Request{
		Text:          text,
		Source:        m.source,
		Target:        target,
		Mode:          ne.Mode,
		Stream:        true, // ignored by non-streaming engines (google/dict)
		Model:         m.modelOverride,
		ModelProvider: m.p.ModelProvider,
		Preset:        m.preset,
		Extra:         m.p.Instructions,
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

// clearResult drops the shown result. Used when a setting (engine/lang/model/
// style/pair) changes without an immediate re-translate, so the pane never shows
// output that's stale relative to the (now different) footer.
func (m *Model) clearResult() {
	m.cancelInflight()
	m.result = nil
	m.streamBuf = ""
	m.err = nil
	m.status = statusIdle
	m.vp.SetContent("")
}

// copyText returns the text to place on the clipboard for the current result.
func (m Model) copyText() string {
	if m.result == nil {
		return ""
	}
	return strings.TrimSpace(m.result.Translation)
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
	case overlaySuggest:
		m.suggestList, cmd = m.suggestList.Update(msg)
	case overlayPreset:
		m.presetList, cmd = m.presetList.Update(msg)
	}
	return cmd
}

// overlaySelect applies the highlighted overlay item and closes the overlay.
func (m Model) overlaySelect() (tea.Model, tea.Cmd) {
	switch m.overlay {
	case overlayHistory:
		// Recall is an explicit "show me this" action: always translate.
		if it, ok := m.hist.SelectedItem().(histItem); ok {
			m.ta.SetValue(it.rec.Input)
			m.ta.MoveToEnd()
			m.overlay = overlayNone
			return m.launch(true)
		}
	case overlayLang:
		if it, ok := m.langList.SelectedItem().(langItem); ok {
			m.target = it.code
			m.overlay = overlayNone
			m.relayout() // pair segment width changed
			if m.live && strings.TrimSpace(m.ta.Value()) != "" {
				return m.launch(true)
			}
			m.clearResult()
			return m, nil
		}
	case overlayModel:
		if it, ok := m.modelList.SelectedItem().(modelItem); ok {
			m.modelOverride = it.id
			m.overlay = overlayNone
			if m.live && strings.TrimSpace(m.ta.Value()) != "" {
				return m.launch(true)
			}
			m.clearResult()
			return m, nil
		}
	case overlaySuggest:
		// Picking a suggestion updates the input and looks it up.
		if it, ok := m.suggestList.SelectedItem().(suggestItem); ok {
			m.ta.SetValue(it.word)
			m.ta.MoveToEnd()
			m.overlay = overlayNone
			return m.launch(true)
		}
	case overlayPreset:
		if it, ok := m.presetList.SelectedItem().(presetItem); ok {
			m.preset = it.id
			m.overlay = overlayNone
			m.relayout() // style segment width changed
			if m.live && strings.TrimSpace(m.ta.Value()) != "" {
				return m.launch(false)
			}
			m.clearResult()
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
	target := m.target
	if res.Target != "" {
		target = res.Target // effective target (may differ in pair mode)
	}
	return store.Record{
		SourceLang:   src,
		TargetLang:   target,
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
	const inputH = 4
	footerH := m.footerHeight() // wraps to width; may be >1 on narrow terminals
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
	m.resultH = resultH
	overlayH := m.height - 2 // overlays use a single-line footer
	if overlayH < 1 {
		overlayH = 1
	}
	m.hist.SetSize(m.width-2, overlayH)
	m.langList.SetSize(m.width-2, overlayH)
	m.modelList.SetSize(m.width-2, overlayH)
	m.suggestList.SetSize(m.width-2, overlayH)
	m.presetList.SetSize(m.width-2, overlayH)
}
