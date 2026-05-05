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

	"github.com/hugs7/bitbucket-cli/internal/api"
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
	case viewConfirmDeletePR:
		km = m.keys.confirmHelp()
	case viewConfirmMerge:
		// The merge confirm card already shows its own footer
		// hints (↑/↓ pick · d/t toggle · y merge · n/esc cancel)
		// so suppress the global help bar to avoid duplication.
		return ""
	case viewPalette:
		km = m.keys.paletteHelp()
	case viewSettings:
		km = m.keys.settingsHelp()
	case viewReviewerSearch:
		// Header inside the overlay already documents space/enter/
		// esc, so suppress the global help bar to avoid duplication.
		return ""
	case viewMessages:
		km = m.keys.messagesHelp()
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
		base = applyReviewContext(base, m.keys, m.myReviewerStatus(it.pr), m.isOwnPR(it.pr), true)
	} else {
		// Empty list: nothing to act on — strip every PR-action key
		// so the help bar doesn't dangle keys that produce a no-op.
		base = applyReviewContext(base, m.keys, "", false, false)
	}
	return base
}

func (m model) contextualDetailHelp() help.KeyMap {
	base := m.keys.detailHelp()
	if it, ok := m.list.SelectedItem().(prItem); ok {
		base = applyReviewContext(base, m.keys, m.myReviewerStatus(it.pr), m.isOwnPR(it.pr), true)
	} else {
		base = applyReviewContext(base, m.keys, "", false, false)
	}
	return base
}

// applyReviewContext rewrites a modeKeyMap so the visible review
// actions match what the current user can usefully do.
//   - hasSelection=false → strip every PR-action key (empty list).
//   - own PR → hide every review action (Bitbucket rejects self-review).
//   - already-approved → hide Approve, surface Unapprove.
//   - already-needs-work → hide NeedsWork, surface Unapprove.
//   - otherwise → show Approve + NeedsWork, hide Unapprove.
func applyReviewContext(km modeKeyMap, k keyMap, status string, ownPR, hasSelection bool) modeKeyMap {
	// Keys that only make sense when there's a PR selected. Stripped
	// wholesale when hasSelection is false.
	prActionKeys := map[string]bool{
		k.Approve.Help().Key:         true,
		k.Unapprove.Help().Key:       true,
		k.NeedsWork.Help().Key:       true,
		k.Merge.Help().Key:           true,
		k.EditDesc.Help().Key:        true,
		k.Comments.Help().Key:        true,
		k.Diff.Help().Key:            true,
		k.Open.Help().Key:            true,
		k.DeclinePR.Help().Key:       true,
		k.ManageReviewers.Help().Key: true,
	}

	allow := func(b key.Binding) bool {
		if !hasSelection && prActionKeys[b.Help().Key] {
			return false
		}
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
	// The toast lives on its own line directly above the help bar so
	// it never squats over the keyboard shortcuts. When there's no
	// toast we drop the line entirely to avoid a phantom blank row.
	if statusLine == "" {
		return body + "\n" + footer
	}
	return body + "\n" + statusLine + "\n" + footer
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
	case viewConfirmDeletePR:
		warn := lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")).
			Bold(true).
			BorderStyle(theme.Border()).
			BorderForeground(lipgloss.Color("9")).
			Padding(0, 2)
		body = "\n  " + warn.Render(fmt.Sprintf(
			"Delete PR #%d?  [y/n]\n  irreversible · Bitbucket Server only · usually requires admin",
			m.pendingDeletePRID))
	case viewConfirmMerge:
		body = "\n" + m.renderMergeConfirm()
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
	case viewMessages:
		// :messages history. Sized to the model's viewport so the
		// list scrolls independently of whatever was on screen
		// before — esc returns the user to that previous view via
		// the navigation stack.
		header := titleBar("MESSAGES",
			theme.TitleChipDim.Render(fmt.Sprintf("%d entries", len(m.messages))),
			theme.TitleChipDim.Render("esc to close"))
		body = header + "\n" + m.messagesVP.View()
	case viewReviewerSearch:
		// Manage-reviewers overlay: a centred bordered card layered
		// on top of the underlying view (PR list / detail) so the
		// modal feels stacked rather than replacing the screen.
		bg := m.renderForMode(m.reviewerSearchReturnTo)
		card := m.renderReviewerSearch()
		body = placeOverlay(bg, card, -1)
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

// renderMergeConfirm draws the merge-confirmation card. Lays the
// strategy picker out as a vertical list (one row per allowed mode)
// rather than the older ←/→ cycle, summarises the post-merge options
// (delete branch, resolve tasks) and lists every open task so the
// user knows exactly what will happen when they press y.
func (m model) renderMergeConfirm() string {
	width := mergeConfirmWidth(m.width)

	header := theme.TitleBadge.Render(fmt.Sprintf(" MERGE PR #%d ", m.pendingMergePRID))
	target := ""
	if it, ok := m.list.SelectedItem().(prItem); ok && it.pr.ID == m.pendingMergePRID {
		target = it.pr.TargetRef
	}
	switch {
	case m.pendingMergeSourceRef != "" && target != "":
		header += "  " + theme.TitleChipDim.Render(m.pendingMergeSourceRef+" → "+target)
	case m.pendingMergeSourceRef != "":
		header += "  " + theme.TitleChipDim.Render(m.pendingMergeSourceRef)
	}

	parts := []string{header, ""}

	// Strategy picker.
	parts = append(parts, theme.TitleChip.Render("Strategy"))
	if n := len(m.pendingMergeStrategies); n == 0 {
		parts = append(parts, "  "+theme.TitleChipDim.Render("(no strategies available — using default)"))
	} else {
		idx := m.pendingMergeStrategyIdx
		if idx < 0 || idx >= n {
			idx = 0
		}
		// Two passes: compute the widest name so the descriptions
		// line up in a column rather than being ragged-right.
		nameW := 0
		for _, st := range m.pendingMergeStrategies {
			if w := lipgloss.Width(st.Name); w > nameW {
				nameW = w
			}
		}
		for i, st := range m.pendingMergeStrategies {
			parts = append(parts, renderStrategyRow(i == idx, st, nameW))
		}
	}

	// Options pane: delete branch + resolve tasks toggles.
	parts = append(parts, "", theme.TitleChip.Render("Options"))
	parts = append(parts, "  "+renderToggle(m.pendingMergeDeleteBranch, "d",
		fmt.Sprintf("delete source branch %q after merge", m.pendingMergeSourceRef)))

	hasTasks := len(m.pendingMergeTasks) > 0
	taskLabel := "resolve all open tasks before merging"
	if !hasTasks && !m.pendingMergeTasksLoading && m.pendingMergeTasksErr == nil {
		taskLabel += "  " + theme.TitleChipDim.Render("(none open)")
	}
	parts = append(parts, "  "+renderToggle(m.pendingMergeResolveTasks, "t", taskLabel))

	// Tasks pane (only when relevant).
	switch {
	case m.pendingMergeTasksLoading:
		parts = append(parts, "", theme.TitleChipDim.Render("  "+m.spinner.View()+" loading tasks…"))
	case m.pendingMergeTasksErr != nil:
		parts = append(parts, "", lipgloss.NewStyle().Foreground(lipgloss.Color("9")).
			Render("  ✗ tasks: "+m.pendingMergeTasksErr.Error()))
	case hasTasks:
		parts = append(parts, "", theme.TitleChipWarn.Render(
			fmt.Sprintf("Open tasks: %d", len(m.pendingMergeTasks))))
		for _, t := range m.pendingMergeTasks {
			text := strings.TrimSpace(strings.SplitN(t.Text, "\n", 2)[0])
			if text == "" {
				text = "(no description)"
			}
			// Trim long task lines so they don't wrap in the box.
			max := width - 12
			if max > 0 && lipgloss.Width(text) > max {
				text = text[:max-1] + "…"
			}
			parts = append(parts, "  "+theme.TitleChipDim.Render("•")+
				" "+theme.TitleChipDim.Render(fmt.Sprintf("#%d", t.ID))+
				"  "+text)
		}
	}

	// Footer hints.
	parts = append(parts, "",
		theme.TitleChipDim.Render(
			"↑/↓ pick strategy   d toggle delete   t toggle tasks   y merge   n/esc cancel"))

	box := lipgloss.NewStyle().
		Border(theme.Border()).
		BorderForeground(lipgloss.Color("10")).
		Padding(1, 2).
		Width(width)
	return box.Render(strings.Join(parts, "\n"))
}

// renderStrategyRow draws one row in the strategy picker. The
// selected row is highlighted as a solid bar; others use a muted
// glyph + name + git command hint.
func renderStrategyRow(selected bool, st api.MergeStrategy, nameW int) string {
	hint := strategyHint(st.ID)
	tag := ""
	if st.Default {
		tag = "  " + theme.TitleChipDim.Render("(repo default)")
	}
	name := st.Name
	if pad := nameW - lipgloss.Width(name); pad > 0 {
		name += strings.Repeat(" ", pad)
	}

	if selected {
		line := "▸ ● " + name + "  " + hint + tag
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("231")).
			Background(lipgloss.Color("22")).
			Bold(true).
			Render(line)
	}
	return "  ○ " + name + "  " + theme.TitleChipDim.Render(hint) + tag
}

// renderToggle formats a "[x] k  label" line for the options pane.
func renderToggle(on bool, key, label string) string {
	check := "[ ]"
	if on {
		check = "[x]"
	}
	return check + " " + theme.TitleChipWarn.Render(key) + "  " + label
}

// strategyHint returns the git-command sketch for a known strategy
// ID. Empty string if unknown — the row still renders, just without
// a hint column.
func strategyHint(id string) string {
	switch id {
	case "no-ff", "merge_commit":
		return "--no-ff"
	case "ff", "fast_forward":
		return "--ff"
	case "ff-only":
		return "--ff-only"
	case "rebase-no-ff":
		return "rebase + merge --no-ff"
	case "rebase-ff-only":
		return "rebase + merge --ff-only"
	case "squash":
		return "--squash"
	case "squash-ff-only":
		return "--squash --ff-only"
	}
	return ""
}

// mergeConfirmWidth picks a roomy width for the merge confirm card
// (capped so it doesn't span ultrawide terminals).
func mergeConfirmWidth(termW int) int {
	const minW, maxW = 60, 90
	w := termW - 4
	if w < minW {
		w = minW
	}
	if w > maxW {
		w = maxW
	}
	return w
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

