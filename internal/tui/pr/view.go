// Package pr — View dispatcher and helpView.
//
// The top-level View() routes to the right per-mode renderer (list,
// detail, diff, comments, palette, editor, confirm dialogs); helpView()
// picks the relevant footer keymap for the current mode.
package pr

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/lipgloss"

	"github.com/hugs7/bitbucket-cli/internal/tui/theme"
)

func (m model) helpView() string {
	var km help.KeyMap
	switch m.mode {
	case viewDiff:
		km = m.keys.viewerHelp()
	case viewDetail:
		km = m.keys.detailHelp()
	case viewComments:
		km = m.keys.commentsHelp()
	case viewConfirmDelete:
		km = m.keys.confirmHelp()
	case viewConfirmDecline:
		km = m.keys.confirmHelp()
	case viewPalette:
		km = m.keys.paletteHelp()
	case viewSettings:
		km = m.keys.settingsHelp()
	default:
		km = m.keys.listHelp()
	}
	return m.help.View(km)
}

// ---------- view ----------

func (m model) View() string {
	if m.err != nil {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("error: "+m.err.Error()) +
			"\n\npress q to quit"
	}

	var body string
	switch m.mode {
	case viewDiff:
		mode := theme.TitleChip.Render("unified")
		if m.diffSplit {
			mode = theme.TitleChip.Render("split")
		}
		overlay := theme.TitleChipDim.Render("comments off")
		if m.diffShowInline {
			overlay = theme.TitleChip.Render("comments on")
		}
		anchor := ""
		if c, ok := m.activeDiffCell(); ok {
			anchor = theme.TitleChipDim.Render(fmt.Sprintf("%s:%d (%s)", c.path, c.line, c.side))
		}
		focus := ""
		if m.diffFocus == "tree" {
			focus = theme.TitleChipWarn.Render("[tree]")
		}
		count := ""
		if m.numBuf != "" {
			count = theme.TitleChipWarn.Render("×" + m.numBuf)
		}
		header := titleBar(
			fmt.Sprintf("DIFF · PR #%d", m.diffID),
			mode, overlay, anchor, focus, count,
		)
		tree := m.renderDiffTree()
		sep := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).
			Render(strings.Repeat("│\n", lipgloss.Height(tree)))
		split := lipgloss.JoinHorizontal(lipgloss.Top, tree, sep, m.diff.View())
		body = header + "\n" + split
	case viewDetail:
		label := "PR DETAIL"
		if it, ok := m.list.SelectedItem().(prItem); ok {
			label = fmt.Sprintf("PR #%d · %s", it.pr.ID, styleState(it.pr.State))
		}
		body = titleBar(label) + "\n" + m.detail.View()
	case viewComments:
		body = titleBar(fmt.Sprintf("COMMENTS · PR #%d", m.commentsPRID),
			theme.TitleChipDim.Render(fmt.Sprintf("%d total", len(m.commentsList)))) + "\n" + m.comments.View()
	case viewConfirmDelete:
		warn := lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")).
			Bold(true).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("9")).
			Padding(0, 2)
		body = "\n  " + warn.Render(fmt.Sprintf("Delete comment #%d?  [y/n]", m.pendingDeleteCommentID))
	case viewConfirmDecline:
		warn := lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")).
			Bold(true).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("9")).
			Padding(0, 2)
		body = "\n  " + warn.Render(fmt.Sprintf("Decline PR #%d?  [y/n]", m.pendingDeclinePRID))
	case viewPalette:
		header := titleBar("COMMAND PALETTE",
			theme.TitleChipDim.Render("type to filter"),
			theme.TitleChipDim.Render("enter runs · esc closes"))
		body = header + "\n" + m.palette.View()
	case viewSettings:
		header := titleBar("SETTINGS",
			theme.TitleChipDim.Render("persisted to ~/.config/bb/config.yml"),
			theme.TitleChipDim.Render("enter / space toggles · esc closes"))
		body = header + "\n" + m.settings.View()
	case viewEditor:
		// The inline editor is centred on top of nothing — we let
		// the overlay own the whole frame so the textarea has room
		// to breathe. The status line still rides along below.
		body = m.editor.view(m.width, m.height-2, m.editor.label())
	default:
		header := titleBar(fmt.Sprintf("PULL REQUESTS · %s/%s", m.project, m.slug),
			theme.TitleChip.Render(strings.ToUpper(m.state)),
			theme.TitleChipDim.Render(fmt.Sprintf("%d shown", len(m.list.Items()))))
		left := lipgloss.NewStyle().Padding(0, 1).Render(m.list.View())
		right := lipgloss.NewStyle().Padding(0, 1).Render(m.detail.View())
		body = header + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	}

	footer := m.helpView()
	statusLine := renderStatusLine(m.loading, m.spinner.View(), m.status)
	return body + "\n" + joinFooter(statusLine, footer)
}

// ---------- helpers ----------

var stateCycle = []string{"OPEN", "MERGED", "DECLINED", "ALL"}

func nextState(s string) string {
	for i, v := range stateCycle {
		if v == strings.ToUpper(s) {
			return stateCycle[(i+1)%len(stateCycle)]
		}
	}
	return stateCycle[0]
}
func prevState(s string) string {
	for i, v := range stateCycle {
		if v == strings.ToUpper(s) {
			return stateCycle[(i-1+len(stateCycle))%len(stateCycle)]
		}
	}
	return stateCycle[0]
}

