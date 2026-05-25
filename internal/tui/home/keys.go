// Package home — keymap.
//
// Pulled out of home.go to keep the model file focused on update
// flow. Bindings are declared once here; help wires them per
// position so the footer stays accurate.
package home

import "github.com/charmbracelet/bubbles/key"

type homeKeys struct {
	Up, Down, Enter, Tab, ShiftTab, Search, Open, CopyLink, Quit, Back, Help, OpenPRs, ToggleFav, ClearStatus, Settings, FocusPane key.Binding
	Approve, Unapprove, NeedsWork, Merge, DeclinePR, DeletePR, ConfirmYes, ConfirmNo                                               key.Binding
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
		CopyLink:    key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy link")),
		OpenPRs:     key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "open PRs")),
		ToggleFav:   key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "favourite")),
		Approve:     key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "approve")),
		Unapprove:   key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "unapprove")),
		NeedsWork:   key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "needs work")),
		Merge:       key.NewBinding(key.WithKeys("M"), key.WithHelp("M", "merge")),
		DeclinePR:   key.NewBinding(key.WithKeys("X"), key.WithHelp("X", "decline PR")),
		DeletePR:    key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "delete PR")),
		ConfirmYes:  key.NewBinding(key.WithKeys("y", "Y", "enter"), key.WithHelp("y", "yes")),
		ConfirmNo:   key.NewBinding(key.WithKeys("n", "N", "esc"), key.WithHelp("n", "no")),
		ClearStatus: key.NewBinding(key.WithKeys("ctrl+l"), key.WithHelp("ctrl+l", "clear status")),
		Settings:    key.NewBinding(key.WithKeys(","), key.WithHelp(",", "settings")),
		Quit:        key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Back:        key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back / blur")),
		Help:        key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		FocusPane:   key.NewBinding(key.WithKeys("T"), key.WithHelp("T", "toggle pane focus")),
	}
}

func (k homeKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Enter, k.Tab, k.FocusPane, k.Search, k.Approve, k.Unapprove, k.NeedsWork, k.Merge, k.OpenPRs, k.Open, k.CopyLink, k.Settings, k.Help, k.Quit}
}
func (k homeKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Enter, k.Tab, k.ShiftTab, k.Search},
		{k.Approve, k.Unapprove, k.NeedsWork, k.Merge, k.DeclinePR, k.DeletePR},
		{k.FocusPane, k.OpenPRs, k.ToggleFav, k.Open, k.CopyLink, k.Settings, k.ClearStatus, k.Help, k.Back, k.Quit},
	}
}
