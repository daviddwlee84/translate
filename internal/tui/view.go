package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"github.com/daviddwlee84/translate/internal/engine"
	"github.com/daviddwlee84/translate/internal/lang"
)

// View renders the two-box layout (input, result) plus a status/help footer.
// AltScreen + mouse are set declaratively (the v2 way).
func (m Model) View() tea.View {
	if !m.ready {
		return altView("initializing…")
	}
	if m.overlay != overlayNone {
		return altView(lg.JoinVertical(lg.Left, m.overlayBody(), m.overlayFooter()))
	}

	inStyle := m.st.input
	if m.ta.Focused() {
		inStyle = m.st.inputHi
	}
	input := inStyle.Width(m.width - 2).Render(m.ta.View())

	// The result box is kept at a fixed height (viewport height) so the layout
	// never jumps between the "translating…" placeholder and streamed output.
	resStyle := m.st.result
	if m.focus == focusOutput {
		resStyle = m.st.resultHi
	}
	result := resStyle.Width(m.width - 2).Height(m.resultH).Render(m.vp.View())
	footer := m.st.footer.Width(m.width).Render(m.statusLine())

	return altView(lg.JoinVertical(lg.Left, input, result, footer))
}

// overlayBody renders the active picker's list (or its loading/empty state).
func (m Model) overlayBody() string {
	switch m.overlay {
	case overlayHistory:
		return m.hist.View()
	case overlayLang:
		return m.langList.View()
	case overlayModel:
		switch {
		case m.modelsLoading:
			return m.sp.View() + " " + m.st.dim.Render("loading models…")
		case m.modelsErr != nil:
			return m.st.warn.Render("⚠ " + m.modelsErr.Error())
		case len(m.cachedModels) == 0:
			return m.st.warn.Render("no models available (is the provider up?)")
		default:
			return m.modelList.View()
		}
	case overlaySuggest:
		return m.suggestList.View()
	case overlayPreset:
		return m.presetList.View()
	}
	return ""
}

func (m Model) overlayFooter() string {
	hint := "↵ select  esc close  ^c quit"
	switch m.overlay {
	case overlayHistory:
		hint = "↵ recall  esc close  ^c quit"
	case overlaySuggest:
		hint = "↵ look up  esc close  ^c quit"
	}
	return m.st.footer.Width(m.width).Render(m.st.dim.Render(hint))
}

// altView wraps content in a full-screen (alt-screen) tea.View with mouse on.
func altView(content string) tea.View {
	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// statusLine renders the footer with the current transient state.
func (m Model) statusLine() string { return m.footerContent(false) }

// footerHeight measures how many rows the footer occupies at the current width.
// It reserves the widest transient state ("translating") so the layout never
// jumps when that segment appears mid-flight.
func (m Model) footerHeight() int {
	if m.width <= 0 {
		return 1
	}
	return lg.Height(m.st.footer.Width(m.width).Render(m.footerContent(true)))
}

// footerContent builds the footer string. forceState reserves the transient
// "translating" segment (for height measurement); otherwise the actual state is
// shown. lipgloss word-wraps it to the width, so narrow terminals get 2+ rows.
func (m Model) footerContent(forceState bool) string {
	live := m.st.liveOff.Render("live○")
	if m.live {
		live = m.st.liveOn.Render("live●")
	}

	var state string
	if forceState {
		state = m.st.dim.Render("⠿ translating")
	} else if m.flash != "" {
		state = m.st.liveOn.Render(m.flash)
	} else {
		switch m.status {
		case statusTranslating:
			state = m.sp.View() + " " + m.st.dim.Render("translating")
		case statusError:
			state = m.st.errText.Render("error")
		}
	}

	// Learn mode: bidirectional tutor. No engine cycle or style (it has its own
	// prompt); shows the direction pair + the LLM that served it.
	if m.learn {
		left := m.st.label.Render(fmt.Sprintf("learn %s⇄%s", m.target, m.pairWith))
		engineSeg := m.curEngine
		if m.curModel != "" {
			if engineSeg != "" {
				engineSeg = fmt.Sprintf("%s (%s)", engineSeg, m.curModel)
			} else {
				engineSeg = m.curModel
			}
		}
		if engineSeg != "" {
			left += m.st.dim.Render(" · ") + m.st.dim.Render(engineSeg)
		}
		left += m.st.dim.Render(" · ") + live
		if state != "" {
			left += "  " + state
		}
		return left + "  " + m.st.dim.Render("↵ learn  ^y copy  ^s speak  ^l live  ^n exit  ^t lang  ^p model  ^u clear  ⇥ focus  ^r history  ^c quit")
	}

	// Dictionary mode: no source→target pair (English-only lookups per script).
	if m.active().Mode == engine.ModeDict {
		left := strings.Join([]string{
			m.st.label.Render("dictionary (zh↔en)"),
			live,
		}, m.st.dim.Render(" · "))
		if state != "" {
			left += "  " + state
		}
		return left + "  " + m.st.dim.Render("↵ define  ^y copy  ^s speak  ^l live  ^e engine  ^u clear  ⇥ focus  ^r history  ^c quit")
	}

	pair := fmt.Sprintf("%s→%s", lang.Name(m.source), lang.Name(m.target))
	if m.pairOn() && m.pairWith != "" {
		switch m.pairMode {
		case pairAway:
			pair = fmt.Sprintf("pair →%s", m.pairWith) // forced to the away language
		case pairHome:
			pair = fmt.Sprintf("pair →%s", m.target) // forced to the home language
		default: // pairAuto
			pair = fmt.Sprintf("pair %s⇄%s", m.target, m.pairWith) // compact codes
		}
	}

	// Engine segment: the selected engine name, plus the model that actually
	// served the last result (once known).
	engineSeg := m.active().Name
	if m.curEngine != "" && m.curEngine != m.active().Name {
		engineSeg = m.curEngine // chain fell back to a specific engine
	}
	if m.curModel != "" {
		engineSeg = fmt.Sprintf("%s (%s)", engineSeg, m.curModel)
	}

	left := strings.Join([]string{
		m.st.label.Render(pair),
		m.st.dim.Render(engineSeg),
		m.st.dim.Render("style:" + m.preset),
		live,
	}, m.st.dim.Render(" · "))
	if state != "" {
		left += "  " + state
	}

	help := m.st.dim.Render("↵ translate  ^y copy  ^s speak  ^l live  ^e engine  ^t lang  ^p model  ^o style  ^g dir  ^u clear  ⇥ focus  ^r history  ^c quit")
	return left + "  " + help
}

// renderResult formats a completed translation, a dictionary entry, or a ranked
// "did you mean" list when a dictionary lookup missed.
func (m Model) renderResult(res engine.TranslateResult) string {
	if res.Learn != nil {
		return m.renderLearn(res)
	}
	if res.Dictionary != nil {
		return m.renderDictionary(res)
	}
	if len(res.Suggestions) > 0 {
		return m.renderSuggestions(res)
	}
	var b strings.Builder
	b.WriteString(m.st.renderTranslation(res.Translation))

	if res.DetectedSource != "" && (m.source == "" || m.source == "auto") {
		b.WriteString("\n" + m.st.dim.Render("detected: "+lang.Name(res.DetectedSource)))
	}
	for _, a := range res.Alternatives {
		b.WriteString("\n" + m.st.alt.Render("~ "+a))
	}
	if res.Notes != "" {
		b.WriteString("\n" + m.st.notes.Render("ⓘ "+res.Notes))
	}
	for _, w := range res.Warnings {
		b.WriteString("\n" + m.st.warn.Render("⚠ "+w))
	}
	// A truncation warning already tells the user to press Enter to retry; the
	// engine-switch hint only fits chain-fallback warnings, so skip it there.
	if len(res.Warnings) > 0 && !res.Truncated {
		b.WriteString("\n" + m.st.dim.Render("(^e switch engine · check the model/provider)"))
	}
	return b.String()
}

// renderDictionary formats a dictionary lookup for the result pane.
func (m Model) renderDictionary(res engine.TranslateResult) string {
	d := res.Dictionary
	var b strings.Builder
	head := d.Word
	if d.Phonetic != "" {
		head += "  " + d.Phonetic
	}
	b.WriteString(m.st.trans.Render(head))
	for _, mn := range d.Meanings {
		b.WriteString("\n" + m.st.notes.Render(mn.PartOfSpeech))
		for i, def := range mn.Definitions {
			if i >= 3 {
				break
			}
			b.WriteString("\n" + m.st.alt.Render("• "+def.Text))
		}
	}
	return b.String()
}

// renderLearn formats a learn-mode result (teach or correct) for the result pane.
func (m Model) renderLearn(res engine.TranslateResult) string {
	l := res.Learn
	var b strings.Builder
	if l.Direction == "correct" {
		corrected := strings.TrimSpace(l.Corrected)
		if corrected == "" {
			corrected = res.Translation
		}
		b.WriteString(m.st.trans.Render("✔ " + corrected))
		if s := strings.TrimSpace(l.Translation); s != "" {
			b.WriteString("\n" + m.st.dim.Render(s))
		}
		for _, is := range l.Issues {
			frag := strings.TrimSpace(is.Span)
			if f := strings.TrimSpace(is.Fix); f != "" {
				frag = strings.TrimSpace(frag + " → " + f)
			}
			if frag != "" && frag != "→" {
				b.WriteString("\n" + m.st.warn.Render("✎ "+frag))
			}
			if e := strings.TrimSpace(is.Explanation); e != "" {
				b.WriteString("\n" + m.st.alt.Render("  "+e))
			}
		}
	} else {
		tr := strings.TrimSpace(l.Translation)
		if tr == "" {
			tr = res.Translation
		}
		b.WriteString(m.st.trans.Render(tr))
		for _, v := range l.Vocab {
			head := v.Term
			if v.Pos != "" {
				head += " (" + v.Pos + ")"
			}
			if v.Phonetic != "" {
				head += " " + v.Phonetic
			}
			b.WriteString("\n" + m.st.notes.Render(head))
			if s := strings.TrimSpace(v.Meaning); s != "" {
				b.WriteString("  " + m.st.alt.Render(s))
			}
		}
		for _, ex := range l.Examples {
			b.WriteString("\n" + m.st.alt.Render("✎ "+ex.Foreign))
			if s := strings.TrimSpace(ex.Native); s != "" {
				b.WriteString("\n" + m.st.dim.Render("  ↳ "+s))
			}
		}
	}
	if s := strings.TrimSpace(l.Notes); s != "" {
		b.WriteString("\n" + m.st.notes.Render("ⓘ "+s))
	}
	for _, w := range res.Warnings {
		b.WriteString("\n" + m.st.warn.Render("⚠ "+w))
	}
	return b.String()
}

// renderSuggestions renders the ranked "did you mean" list for a dictionary miss.
func (m Model) renderSuggestions(res engine.TranslateResult) string {
	var b strings.Builder
	b.WriteString(m.st.dim.Render("no exact match — did you mean:"))
	for i, w := range res.Suggestions {
		b.WriteString("\n" + m.st.dim.Render(fmt.Sprintf("%d.", i+1)) + " " + m.st.alt.Render(w))
	}
	b.WriteString("\n" + m.st.dim.Render("↵ choose"))
	return b.String()
}
