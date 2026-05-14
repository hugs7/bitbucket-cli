// Package pr — settings overlay.
//
// A small in-TUI control panel for the user-facing config toggles
// (inline editor mode, diff split / inline comments, theme). Opens
// over the current view via the `,` keybind or the command palette
// and persists each change to ~/.config/bb/config.yml so it survives
// across sessions.
package pr

import (
	"fmt"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/hugs7/bitbucket-cli/internal/config"
	"github.com/hugs7/bitbucket-cli/internal/tui/theme"
)

// settingItem is one row in the settings list. valueFn renders the
// current value as a chip-suffixed string; toggleFn mutates the model
// (and persists via config) and returns an optional Cmd.
type settingItem struct {
	label    string
	hint     string
	valueFn  func() string
	toggleFn func(m *model) tea.Cmd
}

func (s settingItem) FilterValue() string { return s.label }
func (s settingItem) Title() string {
	// Compose the row title with the current value rendered inline so
	// the user can see all toggles at a glance without expanding rows.
	return s.label + "  " + theme.TitleChip.Render(s.valueFn())
}
func (s settingItem) Description() string {
	if s.hint == "" {
		return ""
	}
	return s.hint
}

// onOff renders a friendly on/off badge for boolean settings.
func onOff(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

// buildSettingsItems assembles the visible toggles. The valueFn
// closures read straight from config so the list stays in sync after
// each toggle without having to mutate item state.
func buildSettingsItems() []list.Item {
	return []list.Item{
		settingItem{
			label:   "Inline (PIP) editor",
			hint:    "Open comments / descriptions in an overlay textarea instead of $EDITOR",
			valueFn: func() string { return onOff(config.Get().InlineEditor) },
			toggleFn: func(m *model) tea.Cmd {
				cur := config.Get().InlineEditor
				if err := config.SetInlineEditor(!cur); err != nil {
					m.status = "✗ save: " + err.Error()
					return nil
				}
				m.status = fmt.Sprintf("✓ inline editor: %s", onOff(!cur))
				return nil
			},
		},
		settingItem{
			label:   "PTY editor (experimental)",
			hint:    "Embed $EDITOR (vim/nvim) inline between diff lines. Off → fullscreen $EDITOR. Unreliable on Windows / WSL — kept off by default.",
			valueFn: func() string { return onOff(config.Get().PTYEditor) },
			toggleFn: func(m *model) tea.Cmd {
				cur := config.Get().PTYEditor
				if err := config.SetPTYEditor(!cur); err != nil {
					m.status = "✗ save: " + err.Error()
					return nil
				}
				m.status = fmt.Sprintf("✓ PTY editor: %s", onOff(!cur))
				return nil
			},
		},
		settingItem{
			label: "Diff view",
			hint:  "Split = side-by-side; Unified = single column",
			valueFn: func() string {
				if config.Get().DiffSplit {
					return "SPLIT"
				}
				return "UNIFIED"
			},
			toggleFn: func(m *model) tea.Cmd {
				m.diffSplit = !m.diffSplit
				_ = config.SetDiffPrefs(m.diffSplit, m.diffShowInline)
				if len(m.diffLines) > 0 {
					m.rebuildDiffRows()
				}
				m.status = fmt.Sprintf("✓ diff view: %s", settingDiffViewLabel())
				return nil
			},
		},
		settingItem{
			label:   "Inline comments overlay",
			hint:    "Show review comments inline in the diff view",
			valueFn: func() string { return onOff(!config.Get().DiffHideInline) },
			toggleFn: func(m *model) tea.Cmd {
				m.diffShowInline = !m.diffShowInline
				_ = config.SetDiffPrefs(m.diffSplit, m.diffShowInline)
				if len(m.diffLines) > 0 {
					m.rebuildDiffRows()
				}
				m.status = fmt.Sprintf("✓ inline comments: %s", onOff(m.diffShowInline))
				return nil
			},
		},
		settingItem{
			label:   "Theme",
			hint:    "Cycle through built-in colour themes",
			valueFn: func() string { return theme.Current.Name },
			toggleFn: func(m *model) tea.Cmd {
				next := theme.Next(theme.Current.Name)
				theme.Apply(theme.Lookup(next))
				return func() tea.Msg { return theme.ChangedMsg{Name: next} }
			},
		},
	}
}

// settingDiffViewLabel mirrors the valueFn for the "Diff view" item
// so the toast text after toggling matches the badge.
func settingDiffViewLabel() string {
	if config.Get().DiffSplit {
		return "SPLIT"
	}
	return "UNIFIED"
}

// openSettings captures the current mode, populates the settings list
// and switches into viewSettings.
func (m *model) openSettings() {
	m.settingsReturnTo = m.mode
	m.settings.SetItems(buildSettingsItems())
	m.settings.SetSize(m.width, m.height-4)
	m.mode = viewSettings
}

// refreshSettings rebuilds the list items so titles re-render after a
// toggle (the value chip is computed at item construction time, so a
// mutation needs a fresh item to be visible).
func (m *model) refreshSettings() {
	idx := m.settings.Index()
	m.settings.SetItems(buildSettingsItems())
	if idx >= 0 && idx < len(m.settings.Items()) {
		m.settings.Select(idx)
	}
}
