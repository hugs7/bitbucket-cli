// Package settings is a tiny in-TUI settings overlay shared by every
// bb sub-TUI (home, repo, pr). It exposes the user-facing config
// toggles (theme, editor mode, …) as a navigable list with
// enter / space to toggle and esc to close.
//
// PR adds extra view-specific toggles on top via its own list; this
// package owns the universal ones so opening "," anywhere lands the
// user on the same surface.
package settings

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hugs7/bitbucket-cli/internal/config"
	"github.com/hugs7/bitbucket-cli/internal/tui/theme"
)

// Item is one row in the settings list. ValueFn renders the current
// value as a chip-suffixed string; ToggleFn flips it (and persists
// via config). Items live independent of any host model so the same
// list can be embedded in home, repo, or pr.
type Item struct {
	Label    string
	Hint     string
	ValueFn  func() string
	ToggleFn func() error
}

func (i Item) FilterValue() string { return i.Label }
func (i Item) Title() string {
	v := ""
	if i.ValueFn != nil {
		v = i.ValueFn()
	}
	return i.Label + "  " + theme.TitleChip.Render(v)
}
func (i Item) Description() string { return i.Hint }

// OnOff renders a friendly on/off badge for boolean settings. Exposed
// so callers building extra Items can stay consistent.
func OnOff(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

// UniversalItems is the list of toggles every TUI exposes (theme +
// editor preferences). Added to by callers that want extra
// view-specific items appended.
func UniversalItems() []list.Item {
	return []list.Item{
		Item{
			Label:   "Theme",
			Hint:    "Cycle through built-in colour themes (default · dracula · solarized-dark · nord · 3270)",
			ValueFn: func() string { return theme.Current.Name },
			ToggleFn: func() error {
				next := theme.Next(theme.Current.Name)
				theme.Apply(theme.Lookup(next))
				return config.SetTheme(next)
			},
		},
		Item{
			Label:   "Inline (PIP) editor",
			Hint:    "Open comments / descriptions in an overlay textarea instead of $EDITOR",
			ValueFn: func() string { return OnOff(config.Get().InlineEditor) },
			ToggleFn: func() error {
				return config.SetInlineEditor(!config.Get().InlineEditor)
			},
		},
		Item{
			Label:   "PTY editor (experimental)",
			Hint:    "Embed $EDITOR (vim/nvim) inline in the diff. Off → fullscreen $EDITOR.",
			ValueFn: func() string { return OnOff(config.Get().PTYEditor) },
			ToggleFn: func() error {
				return config.SetPTYEditor(!config.Get().PTYEditor)
			},
		},
	}
}

// Keymap is the single binding the overlay needs externally
// (toggle). Open / close keys are owned by the host TUI so each can
// scope esc / "," to its own dispatch.
type Keymap struct {
	Toggle key.Binding
}

// DefaultKeymap returns the standard toggle binding (enter or space).
func DefaultKeymap() Keymap {
	return Keymap{
		Toggle: key.NewBinding(
			key.WithKeys("enter", " "),
			key.WithHelp("enter/space", "toggle"),
		),
	}
}

// Model is the lightweight overlay: a list.Model plus a help bar.
// Embed in any host model and route keys to it via Update while the
// overlay is active.
type Model struct {
	list list.Model
	keys Keymap
	help help.Model
	w, h int
}

// New constructs an overlay seeded with the universal items.
func New() Model {
	delegate := list.NewDefaultDelegate()
	l := list.New(UniversalItems(), delegate, 0, 0)
	l.Title = "Settings"
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	l.Styles.Title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	return Model{list: l, keys: DefaultKeymap(), help: help.New()}
}

// SetSize resizes the inner list to the given dimensions. Hosts call
// this from their layout() so the overlay tracks terminal resizes.
func (m *Model) SetSize(w, h int) {
	m.w, m.h = w, h
	m.list.SetSize(w, h-2) // -2 for the help line
}

// Update routes a tea.Msg through the list. KeyMsgs trigger a toggle
// when they match Keymap.Toggle; everything else falls through to
// list navigation. Returns the (possibly mutated) Model plus any Cmd
// produced by the underlying list.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		if key.Matches(km, m.keys.Toggle) {
			if it, ok := m.list.SelectedItem().(Item); ok && it.ToggleFn != nil {
				_ = it.ToggleFn()
				// Rebuild items so the value chip reflects the new
				// state. (Items capture state by closure; re-rendering
				// the list view is what re-evaluates ValueFn.)
				idx := m.list.Index()
				m.list.SetItems(UniversalItems())
				if idx < len(m.list.Items()) {
					m.list.Select(idx)
				}
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// View renders the overlay body (title + list + help line).
func (m Model) View() string {
	hint := theme.TitleChipDim.Render("enter/space toggles · esc closes")
	return m.list.View() + "\n" + hint
}
