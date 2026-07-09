package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"translate/internal/engine"
	"translate/internal/lang"
)

// View renders the two-box layout (input, result) plus a status/help footer.
// AltScreen is set declaratively (the v2 way; there is no WithAltScreen option).
func (m Model) View() tea.View {
	if !m.ready {
		return altView("initializing…")
	}
	if m.showHistory {
		footer := m.st.footer.Width(m.width).Render(m.st.dim.Render("↵ recall  esc close  ^c quit"))
		return altView(lg.JoinVertical(lg.Left, m.hist.View(), footer))
	}

	inStyle := m.st.input
	if m.ta.Focused() {
		inStyle = m.st.inputHi
	}
	input := inStyle.Width(m.width - 2).Render(m.ta.View())
	result := m.st.result.Width(m.width - 2).Render(m.vp.View())
	footer := m.st.footer.Width(m.width).Render(m.statusLine())

	return altView(lg.JoinVertical(lg.Left, input, result, footer))
}

// altView wraps content in a full-screen (alt-screen) tea.View.
func altView(content string) tea.View {
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

// statusLine renders the state segment and the key hints.
func (m Model) statusLine() string {
	pair := fmt.Sprintf("%s→%s", lang.Name(m.source), lang.Name(m.target))

	provider, model := m.p.Provider, m.p.Model
	if m.curEngine != "" {
		provider, model = m.curEngine, m.curModel
	}
	engineSeg := provider
	if model != "" {
		engineSeg = fmt.Sprintf("%s (%s)", model, provider)
	}

	live := m.st.liveOff.Render("live○")
	if m.live {
		live = m.st.liveOn.Render("live●")
	}

	var state string
	switch m.status {
	case statusTranslating:
		state = m.st.dim.Render("…translating")
	case statusError:
		state = m.st.errText.Render("error")
	}

	left := strings.Join([]string{
		m.st.label.Render(pair),
		m.st.dim.Render(engineSeg),
		live,
	}, m.st.dim.Render(" · "))
	if state != "" {
		left += "  " + state
	}

	help := m.st.dim.Render("↵ translate  ^l live  ^u clear  ^c quit")
	return left + "  " + help
}

// renderResult formats a completed translation. In this slice the result is the
// plain translation; alternatives/notes render when a later slice populates them.
func (m Model) renderResult(res engine.TranslateResult) string {
	var b strings.Builder
	b.WriteString(m.st.trans.Render(res.Translation))

	if res.DetectedSource != "" && (m.source == "" || m.source == "auto") {
		b.WriteString("\n" + m.st.dim.Render("detected: "+lang.Name(res.DetectedSource)))
	}
	for _, a := range res.Alternatives {
		b.WriteString("\n" + m.st.alt.Render("~ "+a))
	}
	if res.Notes != "" {
		b.WriteString("\n" + m.st.notes.Render("ⓘ "+res.Notes))
	}
	return b.String()
}
