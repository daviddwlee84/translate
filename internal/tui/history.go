package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/daviddwlee84/translate/internal/store"
)

// histItem adapts a history Record to the bubbles list Item/DefaultItem interface.
type histItem struct{ rec store.Record }

func (h histItem) Title() string {
	return fmt.Sprintf("%s  →  %s", truncate(h.rec.Input, 36), truncate(h.rec.Output, 36))
}
func (h histItem) Description() string {
	eng := h.rec.Engine
	if eng == "" {
		eng = "?"
	}
	return fmt.Sprintf("%s→%s · %s", h.rec.SourceLang, h.rec.TargetLang, eng)
}
func (h histItem) FilterValue() string { return h.rec.Input + " " + h.rec.Output }

// historyLoadedMsg delivers recently-loaded records to the list.
type historyLoadedMsg struct{ items []store.Record }

// loadHistoryCmd loads recent history off the UI goroutine.
func loadHistoryCmd(st store.Store) tea.Cmd {
	return func() tea.Msg {
		if st == nil {
			return historyLoadedMsg{}
		}
		recs, err := st.Recent(context.Background(), 100)
		if err != nil {
			return historyLoadedMsg{}
		}
		return historyLoadedMsg{items: recs}
	}
}

// saveHistoryCmd persists one record off the UI goroutine.
func saveHistoryCmd(st store.Store, rec store.Record) tea.Cmd {
	return func() tea.Msg {
		if st != nil {
			_, _ = st.Add(context.Background(), rec)
		}
		return nil
	}
}

func toListItems(recs []store.Record) []list.Item {
	items := make([]list.Item, len(recs))
	for i, r := range recs {
		items[i] = histItem{rec: r}
	}
	return items
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
