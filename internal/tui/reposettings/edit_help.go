package reposettings

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/hugs7/bitbucket-cli/internal/tui/theme"
)

// footerStatus omits prompts already shown beside the active input.
func (m model) footerStatus() string {
	switch m.mode {
	case modeSettingID, modeSettingMinimum, modeSettingLoading, modeSettingSaving:
		return ""
	}
	return lipgloss.NewStyle().Width(max(1, m.width)).Render(theme.RenderStatusLine(m.loading > 0, m.spinner.View(), m.status))
}

// footerHelp keeps navigation instructions specific to the focused pane or
// editor, and wraps instead of hiding the last controls behind an ellipsis.
func (m model) footerHelp() string {
	var text string
	switch m.mode {
	case modePanels, modePanel:
		text = "↑/↓ sections · Tab/Enter details · PgUp/PgDn scroll"
		if m.mode == modePanel {
			text = "↑/↓ scroll · PgUp/PgDn page · Home/End jump · Tab/← sections"
			if m.activePanel().title == "Webhooks" {
				text = "↑/↓ select webhook · PgUp/PgDn scroll · Tab/← sections"
			}
		}
		if m.editable(m.activePanel()) {
			actions := strings.Split(settingPanelHelp(m.activePanel().title), "\n")[0]
			if m.activePanel().title == "Merge checks" {
				actions += " · m approvals"
			}
			text += "\n" + actions
		}
		text += " · ? help · esc back · q quit"
		if m.help.ShowAll {
			text += "\nr refresh · j/k arrows · h/l panes · Shift+Tab switch pane"
		}
	case modeSettingJSON:
		text = "ctrl+s review changes · esc cancel"
	case modeSettingConfirm:
		text = "y apply · n/esc cancel · ↑/↓ scroll · PgUp/PgDn page"
	case modeConfirmAddWebhook, modeConfirmDelete:
		text = "y apply · n/esc cancel"
	case modeSettingLoading, modeSettingSaving:
		text = "Please wait…"
	default:
		text = "enter continue · esc cancel"
	}
	return lipgloss.NewStyle().Width(max(1, m.width)).Render(text)
}

func settingPanelHelp(title string) string {
	switch title {
	case "Webhooks":
		return "a add · d delete"
	case "Default reviewers":
		return "e edit JSON · a add · d delete (by condition ID)"
	case "Repository permissions", "Group permissions":
		return "a grant · e change · d revoke (username/group name)\nRepository grants only; inherited project/global access may remain."
	case "Branch permissions":
		return "a add · d delete (restriction ID)\nNo in-place update API; use separate explicit create/delete operations."
	case "Access keys":
		return "a register PUBLIC SSH key · e change permission · d revoke\nREPO_READ/REPO_WRITE only. No private keys or token generation."
	case "Hooks":
		return "e edit plugin JSON · a enable · d disable (plugin:hook key)\nMetadata only here. Explicit config editing may reveal secrets."
	case "Auto-decline":
		return "e edit JSON · d restore inheritance"
	case "Default branch":
		return "e edit JSON: id must be refs/heads/<branch>"
	default:
		return "e edit JSON"
	}
}
