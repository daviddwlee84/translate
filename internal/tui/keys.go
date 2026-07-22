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
	PickPreset  key.Binding
	TogglePair  key.Binding
	ToggleLearn key.Binding
	Copy        key.Binding
	Speak       key.Binding
	History     key.Binding
	Clear       key.Binding
	SwitchFocus key.Binding
	Quit        key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Translate:   key.NewBinding(key.WithKeys("enter", "ctrl+enter"), key.WithHelp("↵", "translate")),
		Newline:     key.NewBinding(key.WithKeys("alt+enter"), key.WithHelp("⌥↵", "newline")),
		ToggleLive:  key.NewBinding(key.WithKeys("ctrl+l"), key.WithHelp("^l", "live")),
		CycleEngine: key.NewBinding(key.WithKeys("ctrl+e"), key.WithHelp("^e", "engine")),
		PickLang:    key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("^t", "lang")),
		PickModel:   key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("^p", "model")),
		PickPreset:  key.NewBinding(key.WithKeys("ctrl+o"), key.WithHelp("^o", "style")),
		TogglePair:  key.NewBinding(key.WithKeys("ctrl+g"), key.WithHelp("^g", "dir")),
		ToggleLearn: key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("^n", "learn")),
		Copy:        key.NewBinding(key.WithKeys("ctrl+y"), key.WithHelp("^y", "copy")),
		// ctrl+s is XOFF flow-control only in cooked-mode terminals; Bubble Tea puts
		// the TTY in raw mode (IXON disabled), so it arrives as a normal key here.
		Speak:       key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("^s", "speak")),
		History:     key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("^r", "history")),
		Clear:       key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("^u", "clear")),
		SwitchFocus: key.NewBinding(key.WithKeys("tab", "shift+tab"), key.WithHelp("⇥", "focus")),
		Quit:        key.NewBinding(key.WithKeys("ctrl+c", "esc"), key.WithHelp("^c", "quit")),
	}
}
