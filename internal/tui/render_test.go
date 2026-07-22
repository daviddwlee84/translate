package tui

import (
	"context"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/daviddwlee84/translate/internal/engine"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// driveResult runs a translation result through the REAL Model: window sizing,
// a ChunkDone stream event, renderResult, and the viewport — exactly what the TUI
// does. It returns the blank-line run lengths visible in the result pane.
func driveResult(t *testing.T, translation string) []int {
	t.Helper()
	m := New(context.Background(), Params{
		Engines: []NamedEngine{{Name: "test", Mode: engine.ModeTranslate}},
		Target:  "zh-TW",
	})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = tm.(Model)
	m.inflight = 1 // pretend request #1 is in flight so the ChunkDone isn't dropped
	tm, _ = m.Update(streamMsg{seq: 1, chunk: engine.Chunk{
		Kind:   engine.ChunkDone,
		Result: &engine.TranslateResult{Translation: translation, Target: "zh-TW"},
	}})
	m = tm.(Model)

	lines := strings.Split(ansiRe.ReplaceAllString(m.vp.View(), ""), "\n")
	last := -1
	for i, ln := range lines {
		if strings.TrimSpace(ln) != "" {
			last = i
		}
	}
	return blankRuns(lines[:last+1])
}

// TestResultPaneCollapsesBlankLines drives the real Model end-to-end and asserts a
// translation with multi-blank runs reaches the result pane with at most ONE blank
// line between paragraphs. It uses LONG paragraphs that soft-wrap: the display bug
// only surfaces when a paragraph is wide enough that lipgloss block-padding turns
// blank lines into space runs the viewport re-wraps. It also covers blank lines
// made of non-ASCII whitespace (full-width space, NBSP) or CRLF, which a terminal
// scrape can carry.
func TestResultPaneCollapsesBlankLines(t *testing.T) {
	const (
		ideo = "　" // full-width / ideographic space
		nbsp = " " // non-breaking space
	)
	// Long enough to wrap several times at the test's viewport width (114 cols).
	p1 := strings.Repeat("這是第一個很長的段落用來觸發軟換行", 6) + "。"
	p2 := strings.Repeat("這是第二個同樣很長的段落看看空行", 6) + "。"
	p3 := "第三段結尾。"

	cases := []struct {
		name string
		gap  string
	}{
		{"empty blank lines", "\n\n\n\n"},
		{"full-width space blanks", "\n" + ideo + "\n" + ideo + "\n"},
		{"nbsp blanks", "\n" + nbsp + "\n" + nbsp + "\n"},
		{"crlf blanks", "\r\n\r\n\r\n"},
		{"trailing-space blanks", "\n   \n   \n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := driveResult(t, p1+tc.gap+p2+tc.gap+p3)
			for _, r := range got {
				if r > 1 {
					t.Fatalf("blank run of %d reached the result pane; want max 1 (runs=%v)", r, got)
				}
			}
			if len(got) == 0 {
				t.Fatalf("expected paragraph breaks (single blank lines) to survive, got none")
			}
		})
	}
}

// blankRuns returns the lengths of each maximal run of blank (whitespace-only)
// lines that sit between content lines.
func blankRuns(lines []string) []int {
	var runs []int
	run := 0
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			run++
		} else if run > 0 {
			runs = append(runs, run)
			run = 0
		}
	}
	if run > 0 {
		runs = append(runs, run)
	}
	return runs
}
