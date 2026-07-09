package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"translate/internal/engine"
)

// debounceMsg fires debounce after the last keystroke. It carries the seq it was
// armed with; a newer keystroke bumps m.seq so the stale tick is ignored.
type debounceMsg struct{ seq int }

// streamMsg carries one engine Chunk (token, done, or error), stamped with the
// request's seq so cancelled/superseded events are dropped in O(1).
type streamMsg struct {
	seq   int
	chunk engine.Chunk
}

// armDebounce schedules a debounceMsg after d. tea.Tick fires exactly once, so N
// keystrokes arm N ticks; the seq guard collapses them to the last.
func armDebounce(seq int, d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return debounceMsg{seq: seq} })
}

// waitStream reads the next Chunk from ch and wraps it as a seq-stamped msg. The
// engine contract guarantees a terminal ChunkDone/ChunkError before close, so a
// closed channel without one is surfaced as an error.
func waitStream(ch <-chan engine.Chunk, seq int) tea.Cmd {
	return func() tea.Msg {
		c, ok := <-ch
		if !ok {
			return streamMsg{seq: seq, chunk: engine.Chunk{Kind: engine.ChunkError, Err: engine.ErrNoResult}}
		}
		return streamMsg{seq: seq, chunk: c}
	}
}
