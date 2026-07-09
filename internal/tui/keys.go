package tui

import "charm.land/bubbles/v2/key"

// keyMap holds the TUI key bindings. It grows across slices; slice 4 uses the
// core three, later slices add live-toggle, engine cycle, language swap, etc.
type keyMap struct {
	Translate   key.Binding
	Newline     key.Binding
	ToggleLive  key.Binding
	CycleEngine key.Binding
	PickLang    key.Binding
	PickModel   key.Binding
	History     key.Binding
	Clear       key.Binding
	Quit        key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Translate:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("↵", "translate")),
		Newline:     key.NewBinding(key.WithKeys("alt+enter"), key.WithHelp("⌥↵", "newline")),
		ToggleLive:  key.NewBinding(key.WithKeys("ctrl+l"), key.WithHelp("^l", "live")),
		CycleEngine: key.NewBinding(key.WithKeys("ctrl+e"), key.WithHelp("^e", "engine")),
		PickLang:    key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("^t", "lang")),
		PickModel:   key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("^p", "model")),
		History:     key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("^r", "history")),
		Clear:       key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("^u", "clear")),
		Quit:        key.NewBinding(key.WithKeys("ctrl+c", "esc"), key.WithHelp("^c", "quit")),
	}
}
