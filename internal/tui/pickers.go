package tui

import (
	"context"
	"fmt"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/daviddwlee84/translate/internal/engine"
	"github.com/daviddwlee84/translate/internal/lang"
)

// overlayKind selects which full-screen picker (if any) is showing.
type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayHistory
	overlayLang
	overlayModel
	overlaySuggest
	overlayPreset
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

// --- dictionary "did you mean" suggestions ---

type suggestItem struct{ word string }

func (s suggestItem) Title() string       { return s.word }
func (s suggestItem) Description() string { return "" }
func (s suggestItem) FilterValue() string { return s.word }

func suggestItems(words []string) []list.Item {
	items := make([]list.Item, len(words))
	for i, w := range words {
		items[i] = suggestItem{word: w}
	}
	return items
}

// --- LLM prompt-style presets ---

type presetItem struct {
	id, desc string
	current  bool
}

func (p presetItem) Title() string {
	if p.current {
		return p.id + "  ✓"
	}
	return p.id
}
func (p presetItem) Description() string { return p.desc }
func (p presetItem) FilterValue() string { return p.id }

func presetItems(current string) []list.Item {
	defs := []presetItem{
		{id: engine.PresetConcise, desc: "terse direct translation"},
		{id: engine.PresetContextual, desc: "translations across common senses"},
		{id: engine.PresetDictionary, desc: "translation + example sentences"},
	}
	items := make([]list.Item, len(defs))
	for i, d := range defs {
		d.current = d.id == current
		items[i] = d
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
