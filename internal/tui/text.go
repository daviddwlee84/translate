package tui

import "strings"

// renderTranslation styles a free-text translation for the result pane. It first
// collapses excess blank lines, then applies the style ONE LINE AT A TIME instead
// of to the whole block. lipgloss pads every line of a multi-line block out to the
// widest line's width — so a blank line becomes a run of spaces, which the
// SoftWrap viewport then re-wraps into several blank rows (the "many blank lines"
// display bug). Per-line styling leaves blank lines empty, so paragraph breaks
// stay single. (Copy was already correct: it never styles the text.)
func (s styles) renderTranslation(text string) string {
	lines := strings.Split(collapseBlankLines(text), "\n")
	for i, ln := range lines {
		if ln != "" {
			lines[i] = s.trans.Render(ln)
		}
	}
	return strings.Join(lines, "\n")
}

// collapseBlankLines trims trailing whitespace from each line and collapses any
// run of 2+ blank lines down to a single blank line, so a multi-paragraph
// translation keeps its paragraph breaks without the model's occasional extra
// vertical whitespace piling up in the result pane. A "blank" line is any line
// that is empty once whitespace is stripped — including non-ASCII whitespace such
// as a full-width space (U+3000) or NBSP, and a lone CR from CRLF text, all of
// which a terminal-scrape source can carry on its otherwise-empty rows.
func collapseBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	prevBlank := false
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			if prevBlank {
				continue // drop the extra consecutive blank line
			}
			prevBlank = true
			out = append(out, "") // normalize any whitespace-only line to empty
			continue
		}
		prevBlank = false
		out = append(out, strings.TrimRight(ln, " \t\r"))
	}
	return strings.Join(out, "\n")
}
