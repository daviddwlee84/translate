package tui

import (
	"context"
	"fmt"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"translate/internal/engine"
	"translate/internal/lang"
)

// overlayKind selects which full-screen picker (if any) is showing.
type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayHistory
	overlayLang
	overlayModel
)

// --- target-language picker ---

type langItem struct{ code, name string }

func (l langItem) Title() string       { return l.name }
func (l langItem) Description() string { return l.code }
func (l langItem) FilterValue() string { return l.name + " " + l.code }

// langItems builds the target-language list (no "auto": you translate to a
// concrete language).
func langItems() []list.Item {
	ls := lang.List()
	items := make([]list.Item, 0, len(ls))
	for _, l := range ls {
		items = append(items, langItem{code: l.Code, name: l.Name})
	}
	return items
}

// --- model picker ---

type modelItem struct {
	id      string
	current bool
}

func (m modelItem) Title() string {
	if m.current {
		return m.id + "  ✓"
	}
	return m.id
}
func (m modelItem) Description() string { return "" }
func (m modelItem) FilterValue() string { return m.id }

func modelItems(models []string, current string) []list.Item {
	items := make([]list.Item, len(models))
	for i, id := range models {
		items[i] = modelItem{id: id, current: id == current}
	}
	return items
}

// modelsLoadedMsg delivers the session-cached model list (fetched once).
type modelsLoadedMsg struct {
	models []string
	err    error
}

// fetchModelsCmd loads the model list off the UI goroutine.
func fetchModelsCmd(src engine.ModelLister) tea.Cmd {
	return func() tea.Msg {
		if src == nil {
			return modelsLoadedMsg{err: fmt.Errorf("model list unavailable for this engine")}
		}
		ms, err := src.Models(context.Background())
		return modelsLoadedMsg{models: ms, err: err}
	}
}
