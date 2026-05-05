// Package pr — PR comments page.
//
// The comments view: a scrollable list of every comment on the PR
// (general + inline). Item rendering, list construction, mutation
// helpers, and the comment-at-cursor lookup live here.
package pr

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hugs7/bitbucket-cli/internal/api"
)

// commentItem is the list.Item for the comments view.
type commentItem struct{ c api.Comment }

func (i commentItem) FilterValue() string { return i.c.Text }
func (i commentItem) Title() string {
	when := ""
	if !i.c.CreatedAt.IsZero() {
		when = "  · " + humanTime(i.c.CreatedAt)
	}
	anchor := ""
	if i.c.Inline != nil {
		anchor = fmt.Sprintf("  · %s:%d (%s)", i.c.Inline.Path, i.c.Inline.Line, i.c.Inline.Side)
	}
	return fmt.Sprintf("#%d  %s%s%s", i.c.ID, i.c.Author, when, anchor)
}
func (i commentItem) Description() string {
	first := strings.SplitN(strings.ReplaceAll(i.c.Text, "\r", ""), "\n", 2)[0]
	if len(first) > 200 {
		first = first[:197] + "…"
	}
	return first
}


// commentAtCursor returns the latest inline comment threaded at the
// diff cursor's current anchor (walking back through any annotation
// rows the cursor may be sitting on), or false if none exists.
func (m *model) commentAtCursor() (api.Comment, bool) {
	if m.diffCursor < 0 || m.diffCursor >= len(m.diffRows) {
		return api.Comment{}, false
	}
	idx := m.diffCursor
	for idx >= 0 && m.diffRows[idx].annotation {
		idx--
	}
	if idx < 0 {
		return api.Comment{}, false
	}
	row := m.diffRows[idx]
	if row.fullWidth {
		return api.Comment{}, false
	}
	sideIdx := 0
	if m.diffSplit {
		sideIdx = m.diffCursorSide
	}
	cell := row.cells[sideIdx]
	if !cell.commentable() {
		cell = row.cells[1-sideIdx]
		if !cell.commentable() {
			return api.Comment{}, false
		}
	}
	var found *api.Comment
	for i := range m.diffComments {
		cm := m.diffComments[i]
		if cm.Inline != nil &&
			cm.Inline.Path == cell.path &&
			cm.Inline.Side == cell.side &&
			cm.Inline.Line == cell.line {
			cmCopy := cm
			found = &cmCopy
		}
	}
	if found == nil {
		return api.Comment{}, false
	}
	return *found, true
}


// commentMutation runs a comment-changing API call, then re-fetches the
// comments list and returns commentsLoadedMsg (so the view updates).
func (m *model) commentMutation(prID int, label string, fn func() error) tea.Cmd {
	m.loading = true
	return tea.Batch(m.spinner.Tick, func() tea.Msg {
		if err := fn(); err != nil {
			return actionDoneMsg{text: label, err: err}
		}
		cs, err := m.svc.ListComments(m.project, m.slug, prID)
		if err != nil {
			return actionDoneMsg{text: label + " (reload failed)", err: err}
		}
		return commentsLoadedMsg{id: prID, comments: cs}
	})
}



