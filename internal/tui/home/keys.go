// Package home — keymap.
//
// Pulled out of home.go to keep the model file focused on update
// flow. Bindings are declared once here; help wires them per
// position so the footer stays accurate.
package home

import "github.com/charmbracelet/bubbles/key"

type homeKeys struct {
	Up, Down, Enter, Tab, ShiftTab, Search, Open, Quit, Back, Help, OpenPRs, ToggleFav, ClearStatus, Settings key.Binding
}

func defaultHomeKeys() homeKeys {
	return homeKeys{
		Up:          key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:        key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Enter:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open / load")),
		Tab:         key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next tab")),
		ShiftTab:    key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("⇧tab", "prev tab")),
		Search:      key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		Open:        key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "browser")),
		OpenPRs:     key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "open PRs")),
		ToggleFav:   key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "favourite")),
		ClearStatus: key.NewBinding(key.WithKeys("ctrl+l"), key.WithHelp("ctrl+l", "clear status")),
		Settings:    key.NewBinding(key.WithKeys(","), key.WithHelp(",", "settings")),
		Quit:        key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Back:        key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back / blur")),
		Help:        key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	}
}

func (k homeKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Enter, k.Tab, k.Search, k.OpenPRs, k.ToggleFav, k.Open, k.Settings, k.Help, k.Quit}
}
func (k homeKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Enter, k.Tab, k.ShiftTab, k.Search},
		{k.OpenPRs, k.ToggleFav, k.Open, k.Settings, k.ClearStatus, k.Help, k.Back, k.Quit},
	}
}

