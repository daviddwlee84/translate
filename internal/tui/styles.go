package tui

import lg "charm.land/lipgloss/v2"

// palette (fixed for now; light/dark adaptivity is a later slice).
var (
	colAccent = lg.Color("#7D56F4")
	colDim    = lg.Color("#6C6C6C")
	colErr    = lg.Color("#FF5F87")
	colWarn   = lg.Color("#E0A500")
	colOK     = lg.Color("#5FD787")
	colText   = lg.Color("#D0D0D0")
)

type styles struct {
	input    lg.Style
	inputHi  lg.Style
	result   lg.Style
	resultHi lg.Style
	footer   lg.Style
	label    lg.Style
	trans    lg.Style
	alt      lg.Style
	notes    lg.Style
	warn     lg.Style
	errText  lg.Style
	dim      lg.Style
	liveOn   lg.Style
	liveOff  lg.Style
}

func newStyles() styles {
	box := lg.NewStyle().Border(lg.RoundedBorder()).Padding(0, 1)
	return styles{
		input:    box.BorderForeground(colDim),
		inputHi:  box.BorderForeground(colAccent),
		result:   box.BorderForeground(colDim),
		resultHi: box.BorderForeground(colAccent),
		footer:   lg.NewStyle().Foreground(colDim),
		label:    lg.NewStyle().Foreground(colAccent).Bold(true),
		trans:    lg.NewStyle().Foreground(colText).Bold(true),
		alt:      lg.NewStyle().Foreground(colDim),
		notes:    lg.NewStyle().Foreground(colDim).Italic(true),
		warn:     lg.NewStyle().Foreground(colWarn),
		errText:  lg.NewStyle().Foreground(colErr),
		dim:      lg.NewStyle().Foreground(colDim),
		liveOn:   lg.NewStyle().Foreground(colOK),
		liveOff:  lg.NewStyle().Foreground(colDim),
	}
}
