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
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"

	"github.com/hugs7/bitbucket-cli/internal/tui/theme"
)

func (m model) helpView() string {
	var km help.KeyMap
	switch m.mode {
	case viewDiff:
		km = m.keys.viewerHelp()
	case viewDetail:
		km = m.contextualDetailHelp()
	case viewComments:
		km = m.keys.commentsHelp()
	case viewConfirmDelete:
		km = m.keys.confirmHelp()
	case viewConfirmDecline:
		km = m.keys.confirmHelp()
	case viewConfirmMerge:
		km = m.keys.confirmHelp()
	case viewPalette:
		km = m.keys.paletteHelp()
	case viewSettings:
		km = m.keys.settingsHelp()
	default:
		km = m.contextualListHelp()
	}
	return m.help.View(km)
}

// contextualListHelp / contextualDetailHelp swap Approve ↔ Unapprove
// based on the current user's review status on the selected PR. When
// the user has already approved we hide the no-op Approve action and
// surface Unapprove instead. NeedsWork is shown only when the user
// hasn't already marked the PR as needs-work.
func (m model) contextualListHelp() help.KeyMap {
	base := m.keys.listHelp()
	if it, ok := m.list.SelectedItem().(prItem); ok {
		base = applyReviewContext(base, m.keys, m.myReviewerStatus(it.pr), m.isOwnPR(it.pr))
	}
	return base
}

func (m model) contextualDetailHelp() help.KeyMap {
	base := m.keys.detailHelp()
	if it, ok := m.list.SelectedItem().(prItem); ok {
		base = applyReviewContext(base, m.keys, m.myReviewerStatus(it.pr), m.isOwnPR(it.pr))
	}
	return base
}

// applyReviewContext rewrites a modeKeyMap so the visible review
// actions match what the current user can usefully do. Own PRs hide
// every review action (Bitbucket rejects self-review). Already-
// approved PRs hide Approve and surface Unapprove. PRs already
// flagged needs-work hide NeedsWork.
func applyReviewContext(km modeKeyMap, k keyMap, status string, ownPR bool) modeKeyMap {
	allow := func(b key.Binding) bool {
		if ownPR && (b.Help().Key == k.Approve.Help().Key ||
			b.Help().Key == k.Unapprove.Help().Key ||
			b.Help().Key == k.NeedsWork.Help().Key) {
			return false
		}
		switch b.Help().Key {
		case k.Approve.Help().Key:
			return status != "APPROVED"
		case k.Unapprove.Help().Key:
			// Only show unapprove when there's something to undo —
			// either an explicit approval or a needs-work flag.
			return status == "APPROVED" || status == "NEEDS_WORK"
		case k.NeedsWork.Help().Key:
			return status != "NEEDS_WORK"
		}
		return true
	}
	return modeKeyMap{
		short: filterRows(km.short, allow),
		full:  filterRows(km.full, allow),
	}
}

// filterRows returns a new [][]key.Binding with each row having the
// disallowed bindings stripped out.
func filterRows(rows [][]key.Binding, allow func(key.Binding) bool) [][]key.Binding {
	out := make([][]key.Binding, 0, len(rows))
	for _, row := range rows {
		kept := make([]key.Binding, 0, len(row))
		for _, b := range row {
			if allow(b) {
				kept = append(kept, b)
			}
		}
		if len(kept) > 0 {
			out = append(out, kept)
		}
	}
	return out
}

// ---------- view ----------

func (m model) View() string {
	if m.err != nil {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("error: "+m.err.Error()) +
			"\n\npress q to quit"
	}
	body := m.renderForMode(m.mode)
	footer := m.helpView()
	statusLine := renderStatusLine(m.loading, m.spinner.View(), m.status)
	return body + "\n" + joinFooter(statusLine, footer)
}

// renderForMode produces the body string for a given mode, used both
// by View directly and by the palette overlay (which needs to draw
// the underlying view as its background).
func (m model) renderForMode(mode viewMode) string {
	var body string
	switch mode {
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
			Render(strings.Repeat(theme.VerticalRule()+"\n", lipgloss.Height(tree)))
		// When the PTY editor is active, render it as a fixed pane
		// BELOW the diff viewport instead of injecting into the
		// scrollable content. Inside-the-viewport injection caused
		// the pane to be clipped by viewport.YOffset whenever the
		// cursor row sat near the bottom — making the editor appear
		// "wedged" because the user couldn't see nvim's response.
		right := m.diff.View()
		if m.pty != nil && m.pty.Active() && m.editorReturnTo == viewDiff {
			right += "\n" + m.pty.View(m.diff.Width)
		}
		split := lipgloss.JoinHorizontal(lipgloss.Top, tree, sep, right)
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
			BorderStyle(theme.Border()).
			BorderForeground(lipgloss.Color("9")).
			Padding(0, 2)
		body = "\n  " + warn.Render(fmt.Sprintf("Delete comment #%d?  [y/n]", m.pendingDeleteCommentID))
	case viewConfirmDecline:
		warn := lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")).
			Bold(true).
			BorderStyle(theme.Border()).
			BorderForeground(lipgloss.Color("9")).
			Padding(0, 2)
		body = "\n  " + warn.Render(fmt.Sprintf("Decline PR #%d?  [y/n]", m.pendingDeclinePRID))
	case viewConfirmMerge:
		box := lipgloss.NewStyle().
			Foreground(lipgloss.Color("10")).
			Bold(true).
			BorderStyle(theme.Border()).
			BorderForeground(lipgloss.Color("10")).
			Padding(0, 2)
		check := "[ ]"
		if m.pendingMergeDeleteBranch {
			check = "[x]"
		}
		text := fmt.Sprintf("Merge PR #%d?  [y/n]\n  %s d  delete source branch %q after merge",
			m.pendingMergePRID, check, m.pendingMergeSourceRef)
		body = "\n  " + box.Render(text)
	case viewPalette:
		// Render whatever view we came from as the background, then
		// overlay the palette card on top — gives an "Amp-style"
		// floating modal feel without losing context.
		bg := m.renderForMode(m.paletteReturnTo)
		card := m.palette.View(m.width, m.height-2)
		body = placeOverlay(bg, card, -1)
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
	return body
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

