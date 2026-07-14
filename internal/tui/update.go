package tui

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"

	"github.com/daviddwlee84/translate/internal/debug"
	"github.com/daviddwlee84/translate/internal/engine"
	"github.com/daviddwlee84/translate/internal/lang"
	"github.com/daviddwlee84/translate/internal/store"
	"github.com/daviddwlee84/translate/internal/tts"
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

	case speakDoneMsg:
		m.stopSpeak() // release the cancel; playback has ended
		if msg.err != nil && m.base.Err() == nil {
			m.flash = "speak failed"
			return m, flashCmd()
		}
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

// isTextInput reports whether a key press produced a printable, non-whitespace
// character (Key.Text is set only for printable characters). Space/tab are
// treated as navigation so they can page the viewport instead of typing.
func isTextInput(msg tea.KeyPressMsg) bool {
	return msg.Text != "" && strings.TrimSpace(msg.Text) != ""
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
		m.stopSpeak()
		return m, tea.Quit

	case key.Matches(msg, m.keys.History):
		m.overlay = overlayHistory
		return m, loadHistoryCmd(m.p.Store)

	case key.Matches(msg, m.keys.PickLang):
		m.overlay = overlayLang
		return m, nil

	case key.Matches(msg, m.keys.PickModel):
		if !m.learn && m.active().Mode == engine.ModeDict {
			return m, nil // no model concept in dictionary mode (learn always uses an LLM)
		}
		return m.openModelPicker()

	case key.Matches(msg, m.keys.PickPreset):
		if m.learn || m.active().Mode == engine.ModeDict {
			return m, nil // presets are LLM translate styles; learn has its own prompt
		}
		m.presetList.SetItems(presetItems(m.preset))
		m.overlay = overlayPreset
		return m, nil

	case key.Matches(msg, m.keys.ToggleLearn):
		if m.p.LearnEngine == nil {
			m.flash = "learn needs an LLM provider"
			return m, flashCmd()
		}
		m.learn = !m.learn
		if m.learn {
			// Learn is bidirectional; ensure a distinct "away" (foreign) language.
			m.pair = true
			if m.pairWith == "" || strings.EqualFold(m.pairWith, m.target) {
				if strings.HasPrefix(strings.ToLower(m.target), "en") {
					m.pairWith = "zh-TW"
				} else {
					m.pairWith = "en"
				}
			}
		}
		m.relayout() // footer content changed
		if m.live && strings.TrimSpace(m.ta.Value()) != "" {
			return m.launch(true)
		}
		m.clearResult()
		return m, nil

	case key.Matches(msg, m.keys.TogglePair):
		m.pair = !m.pair
		// Pair mode is a no-op when "away" equals the target; pick a distinct away.
		if m.pair && (m.pairWith == "" || strings.EqualFold(m.pairWith, m.target)) {
			if strings.HasPrefix(strings.ToLower(m.target), "en") {
				m.pairWith = "zh-TW"
			} else {
				m.pairWith = "en"
			}
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

	case key.Matches(msg, m.keys.Speak):
		return m.speak()

	case key.Matches(msg, m.keys.ToggleLive):
		m.live = !m.live
		return m, nil

	case key.Matches(msg, m.keys.CycleEngine):
		if m.learn {
			m.flash = "learn mode (^n to exit)"
			return m, flashCmd()
		}
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
		m.stopSpeak()
		m.seq++
		m.ta.Reset()
		m.focus = focusInput
		m.ta.Focus()
		m.result = nil
		m.err = nil
		m.streamBuf = ""
		m.status = statusIdle
		m.vp.SetContent("")
		return m, nil

	case key.Matches(msg, m.keys.SwitchFocus):
		// Toggle which pane the keyboard drives. Blurring the textarea also dims its
		// border (View keys off ta.Focused()); the result box highlights via m.focus.
		if m.focus == focusInput {
			m.focus = focusOutput
			m.ta.Blur()
			return m, nil
		}
		m.focus = focusInput
		return m, m.ta.Focus()

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

	// Output pane focused: the keyboard scrolls the result viewport. A printable
	// character (not whitespace) snaps focus back to the input and is typed there,
	// so the user can just start typing without pressing Tab first.
	if m.focus == focusOutput {
		if isTextInput(msg) {
			m.focus = focusInput
			cmd := m.ta.Focus()
			var tcmd tea.Cmd
			m.ta, tcmd = m.ta.Update(msg)
			return m, tea.Batch(cmd, tcmd)
		}
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
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
	eng := ne.Engine
	target := m.target
	pairOn := m.pair && m.pairWith != "" && ne.Mode == engine.ModeTranslate
	if pairOn {
		target = lang.PairTarget(m.target, m.pairWith, text) // home-lang input → away, else home
		debug.Logf("tui pair route: home=%s away=%s → target=%s", m.target, m.pairWith, target)
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
		Pair:          pairOn,
		PairHome:      m.target,
		PairAway:      m.pairWith,
	}
	// Learn mode overrides the selected engine with a bare LLM engine and asks for a
	// structured, non-streaming tutor reply routed by pair direction.
	if m.learn && m.p.LearnEngine != nil {
		eng = m.p.LearnEngine
		req.Learn = true
		req.Mode = engine.ModeTranslate
		req.Stream = false
		req.Preset = ""
		req.Pair = true
		req.PairHome = m.target
		req.PairAway = m.pairWith
		req.Target = lang.PairTarget(m.target, m.pairWith, text) // for display/history; engine sets its own
	}
	debug.Logf("tui launch: engine=%s mode=%d target=%s preset=%s learn=%v", eng.Name(), req.Mode, req.Target, m.preset, m.learn)
	ch, err := eng.Translate(ctx, req)
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

// speak plays the foreign side of the current lookup/translation. When the output
// pane is focused it speaks the result explicitly; otherwise it uses the
// foreign-language ("副") preference. Playback runs off the UI thread.
func (m Model) speak() (tea.Model, tea.Cmd) {
	if m.p.Speaker == nil {
		m.flash = "tts disabled"
		return m, flashCmd()
	}
	forced := tts.SideAuto
	if m.focus == focusOutput {
		forced = tts.SideResult // explicit output focus → speak the result pane
	}
	resText, resLang := "", m.target
	if m.result != nil {
		resText = strings.TrimSpace(m.result.Translation)
		if resText == "" && m.result.Dictionary != nil {
			resText = m.result.Dictionary.Word
		}
		if m.result.Target != "" {
			resLang = m.result.Target
		}
	}
	ch, ok := tts.Select(tts.SelectInput{
		SourceText: m.ta.Value(),
		SourceLang: m.source,
		ResultText: resText,
		ResultLang: resLang,
		Foreign:    m.foreignPref(),
		Forced:     forced,
	})
	if !ok {
		m.flash = "nothing to speak"
		return m, flashCmd()
	}
	m.stopSpeak() // debounce: cancel any playback still in flight
	ctx, cancel := context.WithCancel(m.base)
	m.speakCancel = cancel
	m.flash = "speaking…"
	return m, tea.Batch(flashCmd(), speakCmd(m.p.Speaker, ctx, ch))
}

// foreignPref is the preferred foreign/副 language: the configured value, else
// the pair-mode away language ("" => Select derives it).
func (m Model) foreignPref() string {
	if m.p.Foreign != "" {
		return m.p.Foreign
	}
	if m.pair && m.pairWith != "" {
		return m.pairWith
	}
	return ""
}

// stopSpeak cancels any in-flight TTS playback.
func (m *Model) stopSpeak() {
	if m.speakCancel != nil {
		m.speakCancel()
		m.speakCancel = nil
	}
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
// normal mode a click focuses the pane it lands in (so the keyboard scrolls it),
// except a click on the footer row opens the language picker.
func (m Model) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if m.overlay != overlayNone {
		cmd := m.overlayListUpdate(msg)
		return m, cmd
	}
	if m.height > 0 && msg.Y == m.height-1 {
		m.overlay = overlayLang
		return m, nil
	}
	// The input box occupies the first inputH+2 rows (border + inputH + border);
	// everything below it (down to the footer) is the result box.
	if msg.Y < inputH+2 {
		if m.focus != focusInput {
			m.focus = focusInput
			return m, m.ta.Focus()
		}
		return m, nil
	}
	if m.focus != focusOutput {
		m.focus = focusOutput
		m.ta.Blur()
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

// relayout recomputes component sizes for the current window. The input/result
// boxes are rendered at .Width(m.width-2); lipgloss treats that as the TOTAL
// width, so the inner content area is that minus the box frame (border 2 +
// padding 2 = 4) = m.width-6. Wrapping content to anything wider makes each line
// overflow the box and get re-wrapped, orphaning a character per line.
func (m *Model) relayout() {
	if m.width == 0 || m.height == 0 {
		return
	}
	footerH := m.footerHeight() // wraps to width; may be >1 on narrow terminals
	compW := m.width - 6
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
