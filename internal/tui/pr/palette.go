// Package tui — command palette items.
//
// The palette is a fuzzy-searchable list of every action available
// in the current view mode. Pulled out of prs.go to keep the model
// file readable; everything in here is small, declarative, and only
// touches the model through the run-closure callbacks.
package pr

import (
	"fmt"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/hugs7/bitbucket-cli/internal/config"
	"github.com/hugs7/bitbucket-cli/internal/tui/theme"
)


// paletteItem is one entry in the command palette. `run` mutates the
// model and returns a tea.Cmd to fire (or nil for pure state changes).
type paletteItem struct {
	label string
	hint  string
	run   func(m *model) tea.Cmd
}

func (p paletteItem) FilterValue() string { return p.label }
func (p paletteItem) Title() string       { return p.label }
func (p paletteItem) Description() string {
	if p.hint == "" {
		return ""
	}
	return "shortcut: " + p.hint
}

// buildPaletteItems returns the actions relevant for the given mode.
// Items are ordered by frequency of use rather than alphabetically.
func buildPaletteItems(mode viewMode) []list.Item {
	var items []list.Item
	switch mode {
	case viewList, viewDetail:
		items = []list.Item{
			paletteItem{label: "View PR detail", hint: "enter", run: func(m *model) tea.Cmd {
				if _, ok := m.list.SelectedItem().(prItem); ok {
					m.detail.GotoTop()
					m.mode = viewDetail
				}
				return nil
			}},
			paletteItem{label: "View diff", hint: "d", run: func(m *model) tea.Cmd {
				if it, ok := m.list.SelectedItem().(prItem); ok {
					m.loading = true
					return tea.Batch(m.spinner.Tick, m.fetchDiff(it.pr.ID))
				}
				return nil
			}},
			paletteItem{label: "View comments", hint: "c", run: func(m *model) tea.Cmd {
				if it, ok := m.list.SelectedItem().(prItem); ok {
					m.loading = true
					return tea.Batch(m.spinner.Tick, m.fetchComments(it.pr.ID))
				}
				return nil
			}},
			paletteItem{label: "Open in browser", hint: "o", run: func(m *model) tea.Cmd {
				if it, ok := m.list.SelectedItem().(prItem); ok && it.pr.WebURL != "" {
					_ = openInBrowser(it.pr.WebURL)
				}
				return nil
			}},
			paletteItem{label: "Approve PR", hint: "a", run: func(m *model) tea.Cmd {
				if it, ok := m.list.SelectedItem().(prItem); ok {
					if m.isOwnPR(it.pr) {
						m.status = "✗ can't approve your own PR"
						return nil
					}
					id := it.pr.ID
					m.loading = true
					return tea.Batch(m.spinner.Tick, m.doAction(fmt.Sprintf("approved #%d", id), true, func() error {
						return m.svc.ApprovePR(m.project, m.slug, id)
					}))
				}
				return nil
			}},
			paletteItem{label: "Unapprove PR", hint: "A", run: func(m *model) tea.Cmd {
				if it, ok := m.list.SelectedItem().(prItem); ok {
					id := it.pr.ID
					m.loading = true
					return tea.Batch(m.spinner.Tick, m.doAction(fmt.Sprintf("unapproved #%d", id), true, func() error {
						return m.svc.UnapprovePR(m.project, m.slug, id)
					}))
				}
				return nil
			}},
			paletteItem{label: "Mark needs work", hint: "N", run: func(m *model) tea.Cmd {
				if it, ok := m.list.SelectedItem().(prItem); ok {
					id := it.pr.ID
					m.loading = true
					return tea.Batch(m.spinner.Tick, m.doAction(fmt.Sprintf("#%d needs work", id), true, func() error {
						return m.svc.NeedsWorkPR(m.project, m.slug, id)
					}))
				}
				return nil
			}},
			paletteItem{label: "Merge PR", hint: "M", run: func(m *model) tea.Cmd {
				if it, ok := m.list.SelectedItem().(prItem); ok {
					id := it.pr.ID
					m.loading = true
					return tea.Batch(m.spinner.Tick, m.doAction(fmt.Sprintf("merged #%d", id), true, func() error {
						return m.svc.MergePR(m.project, m.slug, id)
					}))
				}
				return nil
			}},
			paletteItem{label: "Edit description", hint: "e", run: func(m *model) tea.Cmd {
				if it, ok := m.list.SelectedItem().(prItem); ok {
					return editInTUI("edit-description",
						fmt.Sprintf("pr-%d-description", it.pr.ID), it.pr.ID, 0, it.pr.Description)
				}
				return nil
			}},
			paletteItem{label: "AI: generate description from diff", hint: "palette", run: func(m *model) tea.Cmd {
				if it, ok := m.list.SelectedItem().(prItem); ok {
					m.loading = true
					m.status = "running AI command…"
					return tea.Batch(m.spinner.Tick, m.fetchAIDescription(it.pr.ID))
				}
				return nil
			}},
			paletteItem{label: "Create new PR", hint: "C", run: func(m *model) tea.Cmd {
				return m.startCreatePR()
			}},
			paletteItem{label: "Decline PR", hint: "X", run: func(m *model) tea.Cmd {
				if it, ok := m.list.SelectedItem().(prItem); ok {
					m.pendingDeclinePRID = it.pr.ID
					m.mode = viewConfirmDecline
					m.paletteReturnTo = viewList
				}
				return nil
			}},
			paletteItem{label: "Add reviewer", hint: "palette", run: func(m *model) tea.Cmd {
				if it, ok := m.list.SelectedItem().(prItem); ok {
					return editInTUI("add-reviewer",
						fmt.Sprintf("pr-%d-add-reviewer", it.pr.ID), it.pr.ID, 0,
						"# Enter one or more usernames (Server) or UUIDs/emails\n"+
							"# (Cloud), separated by space or comma. First non-comment\n"+
							"# line is used. Save & exit to submit; empty cancels.\n")
				}
				return nil
			}},
			paletteItem{label: "Remove reviewer", hint: "palette", run: func(m *model) tea.Cmd {
				if it, ok := m.list.SelectedItem().(prItem); ok {
					hint := ""
					for _, r := range it.pr.Reviewers {
						hint += "# " + r.Username
						if r.DisplayName != "" && r.DisplayName != r.Username {
							hint += "  (" + r.DisplayName + ")"
						}
						hint += "\n"
					}
					if hint == "" {
						hint = "# (no reviewers on this PR)\n"
					}
					return editInTUI("remove-reviewer",
						fmt.Sprintf("pr-%d-remove-reviewer", it.pr.ID), it.pr.ID, 0,
						"# Enter one or more usernames/UUIDs to remove,\n"+
							"# separated by space or comma. Current reviewers:\n"+
							hint)
				}
				return nil
			}},
			paletteItem{label: "Refresh PR list", hint: "r", run: func(m *model) tea.Cmd {
				m.loading = true
				return tea.Batch(m.spinner.Tick, m.fetchPRs())
			}},
			paletteItem{label: "Cycle PR state forward", hint: "s", run: func(m *model) tea.Cmd {
				m.state = nextState(m.state)
				m.list.Title = fmt.Sprintf("Pull Requests · %s/%s · %s", m.project, m.slug, m.state)
				m.loading = true
				return tea.Batch(m.spinner.Tick, m.fetchPRs())
			}},
			paletteItem{label: "Cycle PR state backward", hint: "S", run: func(m *model) tea.Cmd {
				m.state = prevState(m.state)
				m.list.Title = fmt.Sprintf("Pull Requests · %s/%s · %s", m.project, m.slug, m.state)
				m.loading = true
				return tea.Batch(m.spinner.Tick, m.fetchPRs())
			}},
			paletteItem{label: "Back to PR list", hint: "esc", run: func(m *model) tea.Cmd {
				m.paletteReturnTo = viewList
				return nil
			}},
		}
	case viewDiff:
		items = []list.Item{
			paletteItem{label: "Comment current line", hint: "c", run: func(m *model) tea.Cmd {
				c, ok := m.activeDiffCell()
				if !ok {
					m.status = "no file/line under cursor"
					return nil
				}
				return editInlineInTUI(m.diffID, c.path, c.line, c.side)
			}},
			paletteItem{label: "Comment on file (no line)", hint: "N", run: func(m *model) tea.Cmd {
				path := m.currentFilePath()
				if path == "" {
					m.status = "no file under cursor"
					return nil
				}
				return editInlineInTUI(m.diffID, path, 0, "new")
			}},
			paletteItem{label: "PR-level comment", hint: "n", run: func(m *model) tea.Cmd {
				if m.diffID == 0 {
					return nil
				}
				return editInTUI("add-comment-diff",
					fmt.Sprintf("pr-%d-comment", m.diffID), m.diffID, 0, "")
			}},
			paletteItem{label: "Reply to comment at cursor", hint: "r", run: func(m *model) tea.Cmd {
				cm, ok := m.commentAtCursor()
				if !ok {
					m.status = "no comment at cursor"
					return nil
				}
				return editInTUI("reply-inline-comment",
					fmt.Sprintf("pr-%d-reply-to-%d", m.diffID, cm.ID),
					m.diffID, cm.ID, "")
			}},
			paletteItem{label: "Edit comment at cursor", hint: "e", run: func(m *model) tea.Cmd {
				cm, ok := m.commentAtCursor()
				if !ok {
					m.status = "no comment at cursor"
					return nil
				}
				return editInTUI("edit-comment-diff",
					fmt.Sprintf("pr-%d-comment-%d", m.diffID, cm.ID),
					m.diffID, cm.ID, cm.Text)
			}},
			paletteItem{label: "Delete comment at cursor", hint: "D", run: func(m *model) tea.Cmd {
				cm, ok := m.commentAtCursor()
				if !ok {
					m.status = "no comment at cursor"
					return nil
				}
				m.pendingDeleteCommentID = cm.ID
				m.commentsPRID = m.diffID
				m.pendingDeleteFromDiff = true
				m.mode = viewConfirmDelete
				m.paletteReturnTo = viewDiff
				return nil
			}},
			paletteItem{label: "React 👍 to comment at cursor", hint: "R", run: func(m *model) tea.Cmd {
				cm, ok := m.commentAtCursor()
				if !ok {
					m.status = "no comment at cursor"
					return nil
				}
				prID := m.diffID
				cID := cm.ID
				return m.diffCommentMutation(prID, fmt.Sprintf("reacted 👍 to #%d", cID), func() error {
					return m.svc.AddReaction(m.project, m.slug, prID, cID, "thumbsup")
				})
			}},
			paletteItem{label: "Toggle split / unified", hint: "v", run: func(m *model) tea.Cmd {
				m.diffSplit = !m.diffSplit
				m.rebuildDiffRows()
				m.ensureDiffCursorVisible()
				_ = config.SetDiffPrefs(m.diffSplit, m.diffShowInline)
				return nil
			}},
			paletteItem{label: "Toggle inline comments overlay", hint: "i", run: func(m *model) tea.Cmd {
				m.diffShowInline = !m.diffShowInline
				m.rebuildDiffRows()
				m.ensureDiffCursorVisible()
				_ = config.SetDiffPrefs(m.diffSplit, m.diffShowInline)
				return nil
			}},
			paletteItem{label: "Toggle side (split column / context anchor)", hint: "t", run: func(m *model) tea.Cmd {
				if m.diffSplit {
					m.diffCursorSide = 1 - m.diffCursorSide
					m.diff.SetContent(m.renderDiffRows())
				} else if m.diffCursor < len(m.diffLines) {
					dl := &m.diffLines[m.diffCursor]
					if dl.side == "both" {
						dl.preferOld = !dl.preferOld
						m.rebuildDiffRows()
					}
				}
				return nil
			}},
			paletteItem{label: "Focus file tree", hint: "tab", run: func(m *model) tea.Cmd {
				if len(m.diffFiles) > 0 {
					m.diffFocus = "tree"
				}
				return nil
			}},
			paletteItem{label: "Next file", hint: "]", run: func(m *model) tea.Cmd {
				for _, f := range m.diffFiles {
					if f.rowIdx > m.diffCursor {
						m.diffCursor = f.rowIdx
						m.diff.SetContent(m.renderDiffRows())
						m.ensureDiffCursorVisible()
						return nil
					}
				}
				return nil
			}},
			paletteItem{label: "Previous file", hint: "[", run: func(m *model) tea.Cmd {
				for i := len(m.diffFiles) - 1; i >= 0; i-- {
					if m.diffFiles[i].rowIdx < m.diffCursor {
						m.diffCursor = m.diffFiles[i].rowIdx
						m.diff.SetContent(m.renderDiffRows())
						m.ensureDiffCursorVisible()
						return nil
					}
				}
				return nil
			}},
			paletteItem{label: "Go to top of diff", hint: "g", run: func(m *model) tea.Cmd {
				m.diffCursor = 0
				m.diff.SetContent(m.renderDiffRows())
				m.ensureDiffCursorVisible()
				return nil
			}},
			paletteItem{label: "Go to bottom of diff", hint: "G", run: func(m *model) tea.Cmd {
				m.diffCursor = len(m.diffRows) - 1
				m.diff.SetContent(m.renderDiffRows())
				m.ensureDiffCursorVisible()
				return nil
			}},
		}
	case viewComments:
		items = []list.Item{
			paletteItem{label: "Jump to diff at this comment", hint: "enter", run: func(m *model) tea.Cmd {
				if it, ok := m.comments.SelectedItem().(commentItem); ok {
					if it.c.Inline == nil {
						m.status = "no inline anchor — comment is PR-level"
						return nil
					}
					m.diffPendingJump = &diffJumpTarget{
						path: it.c.Inline.Path,
						side: it.c.Inline.Side,
						line: it.c.Inline.Line,
					}
					m.loading = true
					return tea.Batch(m.spinner.Tick, m.fetchDiff(m.commentsPRID))
				}
				return nil
			}},
			paletteItem{label: "Add comment", hint: "n", run: func(m *model) tea.Cmd {
				if m.commentsPRID > 0 {
					return editInTUI("add-comment",
						fmt.Sprintf("pr-%d-comment", m.commentsPRID), m.commentsPRID, 0, "")
				}
				return nil
			}},
			paletteItem{label: "Reply to selected comment", hint: "r", run: func(m *model) tea.Cmd {
				if it, ok := m.comments.SelectedItem().(commentItem); ok {
					return editInTUI("reply-comment",
						fmt.Sprintf("pr-%d-reply-to-%d", m.commentsPRID, it.c.ID),
						m.commentsPRID, it.c.ID, "")
				}
				return nil
			}},
			paletteItem{label: "Edit selected comment", hint: "e", run: func(m *model) tea.Cmd {
				if it, ok := m.comments.SelectedItem().(commentItem); ok {
					return editInTUI("edit-comment",
						fmt.Sprintf("pr-%d-comment-%d", m.commentsPRID, it.c.ID),
						m.commentsPRID, it.c.ID, it.c.Text)
				}
				return nil
			}},
			paletteItem{label: "Delete selected comment", hint: "d", run: func(m *model) tea.Cmd {
				if it, ok := m.comments.SelectedItem().(commentItem); ok {
					m.pendingDeleteCommentID = it.c.ID
					m.mode = viewConfirmDelete
				}
				return nil
			}},
			paletteItem{label: "Refresh comments", hint: "r", run: func(m *model) tea.Cmd {
				if m.commentsPRID > 0 {
					m.loading = true
					return tea.Batch(m.spinner.Tick, m.fetchComments(m.commentsPRID))
				}
				return nil
			}},
		}
	}
	// Universal items appended last so per-mode actions surface first.
	items = append(items,
		paletteItem{label: "Open settings", hint: ",", run: func(m *model) tea.Cmd {
			m.openSettings()
			return nil
		}},
		paletteItem{label: "Switch theme (cycle)", hint: "palette", run: func(m *model) tea.Cmd {
			next := theme.Next(theme.Current.Name)
			theme.Apply(theme.Lookup(next))
			return func() tea.Msg { return theme.ChangedMsg{Name: next} }
		}},
		paletteItem{label: "Toggle help footer", hint: "?", run: func(m *model) tea.Cmd {
			m.help.ShowAll = !m.help.ShowAll
			m.layout()
			return nil
		}},
		paletteItem{label: "Clear status / error", hint: "ctrl+l", run: func(m *model) tea.Cmd {
			m.status = ""
			m.err = nil
			return nil
		}},
		paletteItem{label: "Quit bb", hint: "q / ctrl+c", run: func(m *model) tea.Cmd {
			return tea.Quit
		}},
	)
	return items
}

// openPalette captures the current mode, populates the palette with
// context-aware items and switches into viewPalette.
func (m *model) openPalette() {
	m.paletteReturnTo = m.mode
	m.palette.SetItems(buildPaletteItems(m.paletteReturnTo))
	m.palette.ResetFilter()
	m.palette.SetSize(m.width, m.height-2)
	m.mode = viewPalette
}
