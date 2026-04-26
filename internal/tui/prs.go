// Package tui contains Bubble Tea models for bb's interactive mode.
//
// PRs() returns a runnable program that lets the user browse and act on
// pull requests for a given (host, project, slug).
package tui

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hugo/bb/internal/api"
	"github.com/hugo/bb/internal/config"
)

// PRs launches the interactive pull-requests TUI.
func PRs(svc api.Service, project, slug string) error {
	m := newPRModel(svc, project, slug)
	// WithMouseCellMotion enables wheel + click events so viewports
	// and lists can be scrolled with the mouse.
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

// ---------- model ----------

type viewMode int

const (
	viewList viewMode = iota
	viewDetail
	viewDiff
	viewComments
	viewConfirmDelete
	viewPalette
)

type keyMap struct {
	Up, Down         key.Binding
	Enter            key.Binding
	Diff             key.Binding
	Open             key.Binding
	Refresh          key.Binding
	State, StatePrev key.Binding
	Help             key.Binding
	Back, Quit       key.Binding

	Approve, Unapprove, NeedsWork, Merge key.Binding
	EditDesc, Comments, AddComment       key.Binding

	// comments-mode actions
	EditComment, DeleteComment, ReplyComment, ConfirmYes, ConfirmNo key.Binding

	// diff-mode actions
	InlineComment, ToggleSide, ToggleSplit, ToggleInline key.Binding
	TreeFocus, TreeSelect, NextFile, PrevFile            key.Binding

	// palette
	PaletteOpen key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Up:        key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:      key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Enter:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "view detail")),
		Diff:      key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "diff")),
		Open:      key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "browser")),
		Refresh:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		State:     key.NewBinding(key.WithKeys("s"), key.WithHelp("s/S", "state ←/→")),
		StatePrev: key.NewBinding(key.WithKeys("S")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Back:      key.NewBinding(key.WithKeys("esc", "h"), key.WithHelp("esc/h", "back")),
		Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),

		Approve:    key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "approve")),
		Unapprove:  key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "unapprove")),
		NeedsWork:  key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "needs work")),
		Merge:      key.NewBinding(key.WithKeys("M"), key.WithHelp("M", "merge")),
		EditDesc:   key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit description")),
		Comments:   key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "comments")),
		AddComment: key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new comment")),

		EditComment:   key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
		DeleteComment: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
		ReplyComment:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reply")),
		ConfirmYes:    key.NewBinding(key.WithKeys("y", "Y"), key.WithHelp("y", "yes")),
		ConfirmNo:     key.NewBinding(key.WithKeys("n", "N"), key.WithHelp("n", "no")),

		InlineComment: key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "comment line")),
		ToggleSide:    key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "toggle side")),
		ToggleSplit:   key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "split/unified")),
		ToggleInline:  key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "show/hide comments")),

		TreeFocus:  key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "focus tree/diff")),
		TreeSelect: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open file")),
		NextFile:   key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "next file")),
		PrevFile:   key.NewBinding(key.WithKeys("["), key.WithHelp("[", "prev file")),

		PaletteOpen: key.NewBinding(key.WithKeys("ctrl+k", ":"), key.WithHelp("ctrl+k", "command palette")),
	}
}

// modeKeyMap is a help.KeyMap that exposes only the keys relevant to a
// given view mode, so the footer never lies about what's available.
type modeKeyMap struct {
	short [][]key.Binding
	full  [][]key.Binding
}

func (m modeKeyMap) ShortHelp() []key.Binding {
	if len(m.short) == 0 {
		return nil
	}
	return m.short[0]
}
func (m modeKeyMap) FullHelp() [][]key.Binding { return m.full }

func (k keyMap) listHelp() modeKeyMap {
	return modeKeyMap{
		short: [][]key.Binding{{k.Up, k.Down, k.Enter, k.Diff, k.Comments, k.Approve, k.Merge, k.PaletteOpen, k.Help, k.Quit}},
		full: [][]key.Binding{
			{k.Up, k.Down, k.Enter, k.Diff, k.Open},
			{k.Approve, k.Unapprove, k.NeedsWork, k.Merge},
			{k.EditDesc, k.Comments, k.Refresh, k.State},
			{k.PaletteOpen, k.Help, k.Back, k.Quit},
		},
	}
}
func (k keyMap) viewerHelp() modeKeyMap {
	return modeKeyMap{
		short: [][]key.Binding{{k.Up, k.Down, k.TreeFocus, k.NextFile, k.InlineComment, k.ToggleSplit, k.ToggleInline, k.Back, k.Quit}},
		full: [][]key.Binding{
			{k.Up, k.Down, k.InlineComment, k.ToggleSide},
			{k.TreeFocus, k.TreeSelect, k.PrevFile, k.NextFile},
			{k.ToggleSplit, k.ToggleInline},
			{k.Help, k.Back, k.Quit},
		},
	}
}

// detailHelp surfaces the same action keys as the list, plus scroll/back.
// We want users to act on a PR straight from the detail viewport without
// hopping back to the list first.
func (k keyMap) detailHelp() modeKeyMap {
	return modeKeyMap{
		short: [][]key.Binding{{k.Up, k.Down, k.Diff, k.Comments, k.Approve, k.Merge, k.Back, k.Quit}},
		full: [][]key.Binding{
			{k.Up, k.Down, k.Diff, k.Comments, k.Open},
			{k.Approve, k.Unapprove, k.NeedsWork, k.Merge},
			{k.EditDesc, k.Help, k.Back, k.Quit},
		},
	}
}
func (k keyMap) commentsHelp() modeKeyMap {
	return modeKeyMap{
		short: [][]key.Binding{{k.Up, k.Down, k.AddComment, k.ReplyComment, k.EditComment, k.DeleteComment, k.Back, k.Quit}},
		full: [][]key.Binding{
			{k.Up, k.Down, k.AddComment, k.ReplyComment},
			{k.EditComment, k.DeleteComment, k.Refresh},
			{k.Help, k.Back, k.Quit},
		},
	}
}
func (k keyMap) confirmHelp() modeKeyMap {
	return modeKeyMap{
		short: [][]key.Binding{{k.ConfirmYes, k.ConfirmNo, k.Back}},
		full:  [][]key.Binding{{k.ConfirmYes, k.ConfirmNo, k.Back}},
	}
}
func (k keyMap) paletteHelp() modeKeyMap {
	return modeKeyMap{
		short: [][]key.Binding{{k.Up, k.Down, k.Enter, k.Back}},
		full:  [][]key.Binding{{k.Up, k.Down, k.Enter, k.Back}},
	}
}

type prItem struct{ pr api.PullRequest }

func (i prItem) FilterValue() string { return i.pr.Title }
func (i prItem) Title() string       { return fmt.Sprintf("#%d  %s", i.pr.ID, i.pr.Title) }
func (i prItem) Description() string {
	return fmt.Sprintf("%s · %s → %s · %s", i.pr.State, i.pr.SourceRef, i.pr.TargetRef, i.pr.Author)
}

// ---------- messages ----------

type prsLoadedMsg struct{ prs []api.PullRequest }
type diffLoadedMsg struct {
	id   int
	diff string
}
type commentsLoadedMsg struct {
	id       int
	comments []api.Comment
}
type diffCommentsLoadedMsg struct {
	id       int
	comments []api.Comment
}
type actionDoneMsg struct {
	text string
	err  error
	// reload causes the PR list to refresh after the action.
	reload bool
}
type editorResultMsg struct {
	purpose   string // "edit-description" | "add-comment" | "reply-comment" | "edit-comment" | "add-inline-comment"
	prID      int
	commentID int // for reply-comment (parent) and edit-comment
	text      string
	err       error

	// inline-comment context (only set for "add-inline-comment")
	path string
	line int
	side string // "new" or "old"
}
type errMsg struct{ err error }

// ---------- model ----------

type model struct {
	svc     api.Service
	project string
	slug    string
	state   string

	mode viewMode

	list     list.Model
	detail   viewport.Model
	diff     viewport.Model
	comments list.Model

	commentsList []api.Comment
	commentsPRID int

	// when set, we're in a delete-comment confirm sub-mode
	pendingDeleteCommentID int

	spinner spinner.Model
	help    help.Model
	keys    keyMap

	loading bool
	status  string // transient toast
	err     error

	width, height int
	diffID        int

	// Diff navigation: parsed lines + display rows + cursor position so
	// we can anchor inline comments to the correct (path, side, line).
	// The viewport itself only knows how to scroll a string, so we
	// re-render with a row marker each time the cursor moves.
	diffLines        []diffLine
	diffRows         []diffRow
	diffCursor       int
	diffCursorSide   int  // 0=old (LHS), 1=new (RHS); split mode only
	diffSplit        bool // false=unified, true=side-by-side
	diffShowInline   bool // overlay inline review comments
	diffComments     []api.Comment
	diffCommentsPRID int

	// File-tree side panel: flat list of files (for next/prev-file
	// jumping) plus a hierarchical view (for rendering). The tree
	// cursor indexes into diffTree which interleaves directory headers
	// and file entries.
	diffFiles      []diffFile
	diffTree       []treeNode
	diffTreeCursor int
	diffFocus      string // "diff" or "tree"

	// Vim-style count prefix accumulator (e.g. "15j"). Consumed by the
	// next motion key and reset.
	numBuf string

	// Command palette: a fuzzy-searchable list of context-aware
	// actions. paletteReturnTo is the mode we came from so esc / enter
	// know where to drop back to.
	palette         list.Model
	paletteReturnTo viewMode
}

// diffFile is one entry in the file-tree side panel.
type diffFile struct {
	path   string
	rowIdx int // first row in m.diffRows belonging to this file
}

// treeNode is one row in the hierarchical file-tree view: either a
// directory heading or a file leaf. Files carry the diff row index so
// selection jumps the cursor; dirs are display-only for now.
type treeNode struct {
	name   string // basename for files, dir name for dirs
	path   string // full repo-relative path (files only)
	depth  int    // indentation level (0 = repo root)
	isDir  bool
	rowIdx int // for files: m.diffRows index to jump to
}

// computeFileTree builds a flat list of treeNodes by sorting files
// alphabetically and inserting directory headers wherever the path
// prefix changes from the previous file.
func computeFileTree(files []diffFile) []treeNode {
	if len(files) == 0 {
		return nil
	}
	sorted := make([]diffFile, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].path < sorted[j].path })

	var out []treeNode
	var prev []string
	for _, f := range sorted {
		parts := strings.Split(f.path, "/")
		dirs := parts[:len(parts)-1]
		// Emit dir headers for each directory level that differs from
		// the previous file's path.
		common := 0
		for common < len(dirs) && common < len(prev) && prev[common] == dirs[common] {
			common++
		}
		for i := common; i < len(dirs); i++ {
			out = append(out, treeNode{name: dirs[i], depth: i, isDir: true})
		}
		out = append(out, treeNode{
			name:   parts[len(parts)-1],
			path:   f.path,
			depth:  len(dirs),
			isDir:  false,
			rowIdx: f.rowIdx,
		})
		prev = dirs
	}
	return out
}

func newPRModel(svc api.Service, project, slug string) model {
	delegate := list.NewDefaultDelegate()
	l := list.New(nil, delegate, 0, 0)
	l.Title = fmt.Sprintf("Pull Requests · %s/%s", project, slug)
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.Styles.Title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))

	cdel := list.NewDefaultDelegate()
	cl := list.New(nil, cdel, 0, 0)
	cl.SetShowTitle(false)
	cl.SetShowStatusBar(false)
	cl.SetFilteringEnabled(true)

	pdel := list.NewDefaultDelegate()
	pl := list.New(nil, pdel, 0, 0)
	pl.Title = "Command palette"
	pl.SetShowStatusBar(false)
	pl.SetFilteringEnabled(true)
	pl.SetShowHelp(false)
	pl.Styles.Title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))

	cfg := config.Get()
	return model{
		svc: svc, project: project, slug: slug,
		state:          "OPEN",
		list:           l,
		detail:         viewport.New(0, 0),
		diff:           viewport.New(0, 0),
		comments:       cl,
		palette:        pl,
		spinner:        sp,
		help:           help.New(),
		keys:           defaultKeys(),
		loading:        true,
		diffSplit:      cfg.DiffSplit,
		diffShowInline: !cfg.DiffHideInline,
	}
}

// commentItem is the list.Item for the comments view.
type commentItem struct{ c api.Comment }

func (i commentItem) FilterValue() string { return i.c.Text }
func (i commentItem) Title() string {
	when := ""
	if !i.c.CreatedAt.IsZero() {
		when = "  · " + humanTime(i.c.CreatedAt)
	}
	return fmt.Sprintf("#%d  %s%s", i.c.ID, i.c.Author, when)
}
func (i commentItem) Description() string {
	first := strings.SplitN(strings.ReplaceAll(i.c.Text, "\r", ""), "\n", 2)[0]
	if len(first) > 200 {
		first = first[:197] + "…"
	}
	return first
}

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
		paletteItem{label: "Toggle help footer", hint: "?", run: func(m *model) tea.Cmd {
			m.help.ShowAll = !m.help.ShowAll
			m.layout()
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

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.fetchPRs())
}

// ---------- async commands ----------

func (m *model) fetchPRs() tea.Cmd {
	return func() tea.Msg {
		prs, err := m.svc.ListPRs(m.project, m.slug, m.state, 100)
		if err != nil {
			return errMsg{err}
		}
		return prsLoadedMsg{prs}
	}
}
func (m *model) fetchDiff(id int) tea.Cmd {
	return func() tea.Msg {
		d, err := m.svc.PRDiff(m.project, m.slug, id)
		if err != nil {
			return errMsg{err}
		}
		return diffLoadedMsg{id: id, diff: d}
	}
}
func (m *model) fetchComments(id int) tea.Cmd {
	return func() tea.Msg {
		cs, err := m.svc.ListComments(m.project, m.slug, id)
		if err != nil {
			return errMsg{err}
		}
		return commentsLoadedMsg{id: id, comments: cs}
	}
}

// fetchDiffComments loads comments for the diff overlay. We use a
// dedicated message so an in-flight comments-view fetch doesn't get
// hijacked into the diff (and vice versa).
func (m *model) fetchDiffComments(id int) tea.Cmd {
	return func() tea.Msg {
		cs, err := m.svc.ListComments(m.project, m.slug, id)
		if err != nil {
			// Silently ignore — diff still useful without the overlay.
			return diffCommentsLoadedMsg{id: id, comments: nil}
		}
		return diffCommentsLoadedMsg{id: id, comments: cs}
	}
}
func (m *model) doAction(label string, reload bool, fn func() error) tea.Cmd {
	return func() tea.Msg {
		err := fn()
		return actionDoneMsg{text: label, err: err, reload: reload}
	}
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

// editInTUI suspends the program, opens the user's editor on a temp file
// pre-filled with `initial`, then resumes and dispatches editorResultMsg.
func editInTUI(purpose, hint string, prID, commentID int, initial string) tea.Cmd {
	f, err := os.CreateTemp("", "bb-edit-*-"+hint+".md")
	if err != nil {
		return func() tea.Msg {
			return editorResultMsg{purpose: purpose, prID: prID, commentID: commentID, err: err}
		}
	}
	tmp := f.Name()
	if _, err := f.WriteString(initial); err != nil {
		f.Close()
		os.Remove(tmp)
		return func() tea.Msg {
			return editorResultMsg{purpose: purpose, prID: prID, commentID: commentID, err: err}
		}
	}
	f.Close()

	editor := config.Get().EditorCmd()
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		os.Remove(tmp)
		return func() tea.Msg {
			return editorResultMsg{purpose: purpose, prID: prID, commentID: commentID, err: fmt.Errorf("no editor configured")}
		}
	}
	args := append(parts[1:], tmp)
	cmd := exec.Command(parts[0], args...)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		defer os.Remove(tmp)
		if err != nil {
			return editorResultMsg{purpose: purpose, prID: prID, commentID: commentID, err: err}
		}
		data, rerr := os.ReadFile(tmp)
		return editorResultMsg{purpose: purpose, prID: prID, commentID: commentID, text: string(data), err: rerr}
	})
}

// ---------- update ----------

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		// Re-flow the diff so split columns reshape to the new width.
		if len(m.diffLines) > 0 {
			m.rebuildDiffRows()
			m.ensureDiffCursorVisible()
		}
		return m, nil

	case tea.MouseMsg:
		// Dispatch wheel events (and any future click handling) to
		// whichever component owns the current view. The viewport and
		// list components handle MouseWheelUp/Down natively.
		switch m.mode {
		case viewList:
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			m.updateDetail()
			return m, cmd
		case viewDetail:
			var cmd tea.Cmd
			m.detail, cmd = m.detail.Update(msg)
			return m, cmd
		case viewDiff:
			var cmd tea.Cmd
			m.diff, cmd = m.diff.Update(msg)
			return m, cmd
		case viewComments:
			var cmd tea.Cmd
			m.comments, cmd = m.comments.Update(msg)
			return m, cmd
		case viewPalette:
			var cmd tea.Cmd
			m.palette, cmd = m.palette.Update(msg)
			return m, cmd
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.loading {
			return m, cmd
		}
		return m, nil

	case prsLoadedMsg:
		m.loading = false
		items := make([]list.Item, 0, len(msg.prs))
		for _, p := range msg.prs {
			items = append(items, prItem{p})
		}
		m.list.SetItems(items)
		m.updateDetail()
		return m, nil

	case diffLoadedMsg:
		m.loading = false
		m.diffLines = parseDiff(msg.diff)
		m.diffID = msg.id
		// Pre-fetch comments only the first time we open this PR's
		// diff (or when the PR id changes); cached results are reused
		// on subsequent toggles.
		needComments := m.diffCommentsPRID != msg.id
		if needComments {
			m.diffComments = nil
		}
		m.diffCursor = 0
		m.rebuildDiffRows()
		// Place cursor on the first commentable row.
		for i, r := range m.diffRows {
			if r.fullWidth || r.annotation {
				continue
			}
			if r.cells[0].commentable() || r.cells[1].commentable() {
				m.diffCursor = i
				break
			}
		}
		m.diff.SetContent(m.renderDiffRows())
		m.diff.GotoTop()
		m.ensureDiffCursorVisible()
		m.mode = viewDiff
		if needComments {
			return m, m.fetchDiffComments(msg.id)
		}
		return m, nil

	case diffCommentsLoadedMsg:
		m.loading = false
		m.diffCommentsPRID = msg.id
		m.diffComments = msg.comments
		if m.mode == viewDiff && m.diffID == msg.id {
			m.rebuildDiffRows()
			m.ensureDiffCursorVisible()
		}
		return m, nil

	case commentsLoadedMsg:
		m.loading = false
		m.commentsList = msg.comments
		m.commentsPRID = msg.id
		items := make([]list.Item, 0, len(msg.comments))
		for _, c := range msg.comments {
			items = append(items, commentItem{c})
		}
		m.comments.SetItems(items)
		m.mode = viewComments
		return m, nil

	case actionDoneMsg:
		m.loading = false
		if msg.err != nil {
			m.status = "✗ " + msg.text + ": " + msg.err.Error()
		} else {
			m.status = "✓ " + msg.text
		}
		if msg.reload {
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, m.fetchPRs())
		}
		return m, nil

	case editorResultMsg:
		text := strings.TrimSpace(msg.text)
		if msg.err != nil {
			m.status = "✗ editor: " + msg.err.Error()
			return m, nil
		}
		if text == "" {
			m.status = "aborted (empty)"
			return m, nil
		}
		switch msg.purpose {
		case "edit-description":
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, m.doAction("description updated", true, func() error {
				return m.svc.UpdatePRDescription(m.project, m.slug, msg.prID, text)
			}))
		case "add-comment":
			return m, m.commentMutation(msg.prID, "added comment", func() error {
				_, err := m.svc.AddComment(m.project, m.slug, msg.prID, text)
				return err
			})
		case "reply-comment":
			parent := msg.commentID
			return m, m.commentMutation(msg.prID, fmt.Sprintf("replied to #%d", parent), func() error {
				_, err := m.svc.ReplyComment(m.project, m.slug, msg.prID, parent, text)
				return err
			})
		case "edit-comment":
			cID := msg.commentID
			return m, m.commentMutation(msg.prID, fmt.Sprintf("edited #%d", cID), func() error {
				_, err := m.svc.EditComment(m.project, m.slug, msg.prID, cID, text)
				return err
			})
		case "add-inline-comment":
			path := msg.path
			line := msg.line
			side := msg.side
			prID := msg.prID
			label := fmt.Sprintf("inline comment on %s:%d (%s)", path, line, side)
			m.loading = true
			// After posting, refresh the overlay so the new annotation
			// appears under the diff line right away.
			return m, tea.Batch(m.spinner.Tick, func() tea.Msg {
				_, err := m.svc.AddInlineComment(m.project, m.slug, prID, api.InlineCommentInput{
					Text: text, Path: path, Line: line, Side: side,
				})
				if err != nil {
					return actionDoneMsg{text: label, err: err}
				}
				cs, lerr := m.svc.ListComments(m.project, m.slug, prID)
				if lerr != nil {
					return actionDoneMsg{text: label + " (overlay reload failed)", err: lerr}
				}
				return diffCommentsLoadedMsg{id: prID, comments: cs}
			})
		}
		return m, nil

	case errMsg:
		m.loading = false
		m.err = msg.err
		return m, nil

	case tea.KeyMsg:
		if m.list.FilterState() == list.Filtering {
			break
		}

		// Palette mode owns the keymap entirely while open.
		if m.mode == viewPalette {
			switch {
			case key.Matches(msg, m.keys.Quit):
				return m, tea.Quit
			case key.Matches(msg, m.keys.Back):
				m.mode = m.paletteReturnTo
				return m, nil
			case msg.String() == "enter":
				if it, ok := m.palette.SelectedItem().(paletteItem); ok {
					m.mode = m.paletteReturnTo
					return m, it.run(&m)
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.palette, cmd = m.palette.Update(msg)
			return m, cmd
		}

		// Mode-independent keys come first.
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.PaletteOpen):
			// ":" colon also opens the palette but we must not let it
			// trigger when the user is filtering a list (see top-of
			// block check). Skip in viewDiff if a count is being typed
			// (rare; keep simple by checking numBuf).
			m.openPalette()
			return m, nil
		case key.Matches(msg, m.keys.Back):
			if m.mode != viewList {
				m.mode = viewList
				return m, nil
			}
			return m, tea.Quit
		case key.Matches(msg, m.keys.Help):
			m.help.ShowAll = !m.help.ShowAll
			m.layout()
			return m, nil
		}

		// Per-mode handling.
		switch m.mode {
		case viewDiff:
			s := msg.String()
			n := len(m.diffRows)

			// Vim-style count prefix: digits accumulate into m.numBuf
			// and the next motion key consumes them. "0" with an empty
			// buffer is ignored so it can later be repurposed as a
			// motion if needed (currently no-op).
			if len(s) == 1 && s[0] >= '0' && s[0] <= '9' && !(s == "0" && m.numBuf == "") {
				m.numBuf += s
				return m, nil
			}
			count := 1
			if m.numBuf != "" {
				if v, err := strconv.Atoi(m.numBuf); err == nil && v > 0 {
					count = v
				}
				m.numBuf = ""
			}

			// Tree-focused navigation: j/k move through tree nodes
			// (dirs + files), enter / l on a file jumps the diff and
			// returns focus to the diff viewport.
			if m.diffFocus == "tree" {
				moveTree := func(delta int) {
					if len(m.diffTree) == 0 {
						return
					}
					m.diffTreeCursor += delta
					if m.diffTreeCursor < 0 {
						m.diffTreeCursor = 0
					}
					if m.diffTreeCursor >= len(m.diffTree) {
						m.diffTreeCursor = len(m.diffTree) - 1
					}
				}
				switch {
				case key.Matches(msg, m.keys.Up):
					moveTree(-count)
					return m, nil
				case key.Matches(msg, m.keys.Down):
					moveTree(count)
					return m, nil
				case key.Matches(msg, m.keys.TreeSelect), s == "l":
					if m.diffTreeCursor < len(m.diffTree) {
						node := m.diffTree[m.diffTreeCursor]
						if !node.isDir {
							m.diffCursor = node.rowIdx
							m.diffFocus = "diff"
							m.diff.SetContent(m.renderDiffRows())
							m.ensureDiffCursorVisible()
						}
					}
					return m, nil
				case key.Matches(msg, m.keys.TreeFocus):
					m.diffFocus = "diff"
					return m, nil
				}
				// In tree focus, swallow other keys silently so they
				// don't drift into the diff viewport.
				return m, nil
			}

			move := func(delta int) {
				m.diffCursor += delta
				if m.diffCursor < 0 {
					m.diffCursor = 0
				}
				if m.diffCursor > n-1 {
					m.diffCursor = n - 1
				}
				m.syncTreeCursor()
				m.diff.SetContent(m.renderDiffRows())
				m.ensureDiffCursorVisible()
			}

			switch {
			case key.Matches(msg, m.keys.Up):
				move(-count)
				return m, nil
			case key.Matches(msg, m.keys.Down):
				move(count)
				return m, nil
			case s == "pgup":
				step := m.diff.Height
				if step < 1 {
					step = 1
				}
				move(-step * count)
				return m, nil
			case s == "pgdown", s == " ":
				step := m.diff.Height
				if step < 1 {
					step = 1
				}
				move(step * count)
				return m, nil
			case s == "ctrl+d":
				step := m.diff.Height / 2
				if step < 1 {
					step = 1
				}
				move(step * count)
				return m, nil
			case s == "ctrl+u":
				step := m.diff.Height / 2
				if step < 1 {
					step = 1
				}
				move(-step * count)
				return m, nil
			case s == "g":
				m.diffCursor = 0
				m.syncTreeCursor()
				m.diff.SetContent(m.renderDiffRows())
				m.ensureDiffCursorVisible()
				return m, nil
			case s == "G":
				m.diffCursor = n - 1
				m.syncTreeCursor()
				m.diff.SetContent(m.renderDiffRows())
				m.ensureDiffCursorVisible()
				return m, nil
			case key.Matches(msg, m.keys.NextFile):
				idx := -1
				for i, f := range m.diffFiles {
					if f.rowIdx > m.diffCursor {
						idx = i
						break
					}
				}
				if idx < 0 && len(m.diffFiles) > 0 {
					idx = len(m.diffFiles) - 1
				}
				if idx >= 0 {
					m.diffCursor = m.diffFiles[idx].rowIdx
					m.syncTreeCursor()
					m.diff.SetContent(m.renderDiffRows())
					m.ensureDiffCursorVisible()
				}
				return m, nil
			case key.Matches(msg, m.keys.PrevFile):
				idx := -1
				for i := len(m.diffFiles) - 1; i >= 0; i-- {
					if m.diffFiles[i].rowIdx < m.diffCursor {
						idx = i
						break
					}
				}
				if idx < 0 && len(m.diffFiles) > 0 {
					idx = 0
				}
				if idx >= 0 {
					m.diffCursor = m.diffFiles[idx].rowIdx
					m.syncTreeCursor()
					m.diff.SetContent(m.renderDiffRows())
					m.ensureDiffCursorVisible()
				}
				return m, nil
			case key.Matches(msg, m.keys.TreeFocus):
				if len(m.diffFiles) == 0 {
					m.status = "no files in diff"
					return m, nil
				}
				m.diffFocus = "tree"
				return m, nil
			case key.Matches(msg, m.keys.ToggleSplit):
				m.diffSplit = !m.diffSplit
				m.rebuildDiffRows()
				m.ensureDiffCursorVisible()
				_ = config.SetDiffPrefs(m.diffSplit, m.diffShowInline)
				if m.diffSplit {
					m.status = "split view"
				} else {
					m.status = "unified view"
				}
				return m, nil
			case key.Matches(msg, m.keys.ToggleInline):
				m.diffShowInline = !m.diffShowInline
				m.rebuildDiffRows()
				m.ensureDiffCursorVisible()
				_ = config.SetDiffPrefs(m.diffSplit, m.diffShowInline)
				if m.diffShowInline {
					m.status = "inline comments shown"
				} else {
					m.status = "inline comments hidden"
				}
				return m, nil
			case key.Matches(msg, m.keys.ToggleSide):
				if m.diffSplit {
					m.diffCursorSide = 1 - m.diffCursorSide
					m.diff.SetContent(m.renderDiffRows())
					if c, ok := m.activeDiffCell(); ok {
						m.status = fmt.Sprintf("anchor → %s:%d (%s)", c.path, c.line, c.side)
					}
				} else if m.diffCursor < len(m.diffLines) {
					// Unified mode: only context lines have two sides.
					// We need to find the source diffLine corresponding
					// to this row. In unified, rows map 1:1 to lines.
					if m.diffCursor < len(m.diffLines) {
						dl := &m.diffLines[m.diffCursor]
						if dl.side == "both" {
							dl.preferOld = !dl.preferOld
							m.rebuildDiffRows()
							side, lineNo := inlineSide(*dl)
							m.status = fmt.Sprintf("anchor → %s side L%d", side, lineNo)
						} else {
							m.status = "side toggle only applies to context lines"
						}
					}
				}
				return m, nil
			case key.Matches(msg, m.keys.InlineComment):
				c, ok := m.activeDiffCell()
				if !ok {
					m.status = "no file/line under cursor — move to a +/-/context line"
					return m, nil
				}
				return m, editInlineInTUI(m.diffID, c.path, c.line, c.side)
			}
			// Anything else (including raw scroll keys we didn't match)
			// falls through to the viewport for default behaviour.
			var cmd tea.Cmd
			m.diff, cmd = m.diff.Update(msg)
			return m, cmd
		case viewDetail:
			// Intercept PR action keys so users can act directly from
			// the detail view (mirroring viewList behaviour) without
			// going back to the list first. Scroll keys (↑/↓ j/k pgup/
			// pgdown) don't collide with our action keys, so anything
			// not handled below falls through to the viewport.
			if it, ok := m.list.SelectedItem().(prItem); ok {
				id := it.pr.ID
				switch {
				case key.Matches(msg, m.keys.Diff):
					m.loading = true
					return m, tea.Batch(m.spinner.Tick, m.fetchDiff(id))
				case key.Matches(msg, m.keys.Comments):
					m.loading = true
					return m, tea.Batch(m.spinner.Tick, m.fetchComments(id))
				case key.Matches(msg, m.keys.Open):
					if it.pr.WebURL != "" {
						_ = openInBrowser(it.pr.WebURL)
					}
					return m, nil
				case key.Matches(msg, m.keys.Approve):
					m.loading = true
					return m, tea.Batch(m.spinner.Tick, m.doAction(fmt.Sprintf("approved #%d", id), true, func() error {
						return m.svc.ApprovePR(m.project, m.slug, id)
					}))
				case key.Matches(msg, m.keys.Unapprove):
					m.loading = true
					return m, tea.Batch(m.spinner.Tick, m.doAction(fmt.Sprintf("unapproved #%d", id), true, func() error {
						return m.svc.UnapprovePR(m.project, m.slug, id)
					}))
				case key.Matches(msg, m.keys.NeedsWork):
					m.loading = true
					return m, tea.Batch(m.spinner.Tick, m.doAction(fmt.Sprintf("#%d needs work", id), true, func() error {
						return m.svc.NeedsWorkPR(m.project, m.slug, id)
					}))
				case key.Matches(msg, m.keys.Merge):
					m.loading = true
					return m, tea.Batch(m.spinner.Tick, m.doAction(fmt.Sprintf("merged #%d", id), true, func() error {
						return m.svc.MergePR(m.project, m.slug, id)
					}))
				case key.Matches(msg, m.keys.EditDesc):
					return m, editInTUI("edit-description",
						fmt.Sprintf("pr-%d-description", id), id, 0, it.pr.Description)
				}
			}
			var cmd tea.Cmd
			m.detail, cmd = m.detail.Update(msg)
			return m, cmd
		case viewConfirmDelete:
			switch {
			case key.Matches(msg, m.keys.ConfirmYes):
				cID := m.pendingDeleteCommentID
				prID := m.commentsPRID
				m.pendingDeleteCommentID = 0
				m.mode = viewComments
				return m, m.commentMutation(prID, fmt.Sprintf("deleted #%d", cID), func() error {
					return m.svc.DeleteComment(m.project, m.slug, prID, cID)
				})
			case key.Matches(msg, m.keys.ConfirmNo):
				m.pendingDeleteCommentID = 0
				m.mode = viewComments
				m.status = "delete cancelled"
				return m, nil
			}
			return m, nil

		case viewComments:
			// Filtering input: don't intercept keys.
			if m.comments.FilterState() == list.Filtering {
				break
			}
			switch {
			case key.Matches(msg, m.keys.AddComment):
				if m.commentsPRID > 0 {
					return m, editInTUI("add-comment",
						fmt.Sprintf("pr-%d-comment", m.commentsPRID), m.commentsPRID, 0, "")
				}
			case key.Matches(msg, m.keys.ReplyComment):
				if it, ok := m.comments.SelectedItem().(commentItem); ok {
					return m, editInTUI("reply-comment",
						fmt.Sprintf("pr-%d-reply-to-%d", m.commentsPRID, it.c.ID),
						m.commentsPRID, it.c.ID, "")
				}
			case key.Matches(msg, m.keys.EditComment):
				if it, ok := m.comments.SelectedItem().(commentItem); ok {
					return m, editInTUI("edit-comment",
						fmt.Sprintf("pr-%d-comment-%d", m.commentsPRID, it.c.ID),
						m.commentsPRID, it.c.ID, it.c.Text)
				}
			case key.Matches(msg, m.keys.DeleteComment):
				if it, ok := m.comments.SelectedItem().(commentItem); ok {
					m.pendingDeleteCommentID = it.c.ID
					m.mode = viewConfirmDelete
					return m, nil
				}
			case key.Matches(msg, m.keys.Refresh):
				if m.commentsPRID > 0 {
					m.loading = true
					return m, tea.Batch(m.spinner.Tick, m.fetchComments(m.commentsPRID))
				}
			}
			var cmd tea.Cmd
			m.comments, cmd = m.comments.Update(msg)
			return m, cmd
		}

		// viewList handling.
		switch {
		case key.Matches(msg, m.keys.Refresh):
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, m.fetchPRs())
		case key.Matches(msg, m.keys.StatePrev):
			m.state = prevState(m.state)
			m.list.Title = fmt.Sprintf("Pull Requests · %s/%s · %s", m.project, m.slug, m.state)
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, m.fetchPRs())
		case key.Matches(msg, m.keys.State):
			m.state = nextState(m.state)
			m.list.Title = fmt.Sprintf("Pull Requests · %s/%s · %s", m.project, m.slug, m.state)
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, m.fetchPRs())
		case key.Matches(msg, m.keys.Enter):
			if _, ok := m.list.SelectedItem().(prItem); ok {
				m.detail.GotoTop()
				m.mode = viewDetail
				return m, nil
			}
		case key.Matches(msg, m.keys.Diff):
			if it, ok := m.list.SelectedItem().(prItem); ok {
				m.loading = true
				return m, tea.Batch(m.spinner.Tick, m.fetchDiff(it.pr.ID))
			}
		case key.Matches(msg, m.keys.Comments):
			if it, ok := m.list.SelectedItem().(prItem); ok {
				m.loading = true
				return m, tea.Batch(m.spinner.Tick, m.fetchComments(it.pr.ID))
			}
		case key.Matches(msg, m.keys.Open):
			if it, ok := m.list.SelectedItem().(prItem); ok && it.pr.WebURL != "" {
				_ = openInBrowser(it.pr.WebURL)
			}
		case key.Matches(msg, m.keys.Approve):
			if it, ok := m.list.SelectedItem().(prItem); ok {
				id := it.pr.ID
				m.loading = true
				return m, tea.Batch(m.spinner.Tick, m.doAction(fmt.Sprintf("approved #%d", id), true, func() error {
					return m.svc.ApprovePR(m.project, m.slug, id)
				}))
			}
		case key.Matches(msg, m.keys.Unapprove):
			if it, ok := m.list.SelectedItem().(prItem); ok {
				id := it.pr.ID
				m.loading = true
				return m, tea.Batch(m.spinner.Tick, m.doAction(fmt.Sprintf("unapproved #%d", id), true, func() error {
					return m.svc.UnapprovePR(m.project, m.slug, id)
				}))
			}
		case key.Matches(msg, m.keys.NeedsWork):
			if it, ok := m.list.SelectedItem().(prItem); ok {
				id := it.pr.ID
				m.loading = true
				return m, tea.Batch(m.spinner.Tick, m.doAction(fmt.Sprintf("#%d needs work", id), true, func() error {
					return m.svc.NeedsWorkPR(m.project, m.slug, id)
				}))
			}
		case key.Matches(msg, m.keys.Merge):
			if it, ok := m.list.SelectedItem().(prItem); ok {
				id := it.pr.ID
				m.loading = true
				return m, tea.Batch(m.spinner.Tick, m.doAction(fmt.Sprintf("merged #%d", id), true, func() error {
					return m.svc.MergePR(m.project, m.slug, id)
				}))
			}
		case key.Matches(msg, m.keys.EditDesc):
			if it, ok := m.list.SelectedItem().(prItem); ok {
				return m, editInTUI("edit-description",
					fmt.Sprintf("pr-%d-description", it.pr.ID), it.pr.ID, 0, it.pr.Description)
			}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	m.updateDetail()
	return m, cmd
}

func (m *model) updateDetail() {
	it, ok := m.list.SelectedItem().(prItem)
	if !ok {
		m.detail.SetContent("")
		return
	}
	p := it.pr
	b := lipgloss.NewStyle().Bold(true)
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	branch := lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	author := lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	state := styleState(p.State)

	var sb strings.Builder
	fmt.Fprintf(&sb, "%s  %s\n", b.Render(fmt.Sprintf("#%d", p.ID)), b.Render(p.Title))
	fmt.Fprintf(&sb, "%s · %s · %s → %s\n",
		state, author.Render(p.Author), branch.Render(p.SourceRef), branch.Render(p.TargetRef))
	if !p.UpdatedAt.IsZero() {
		fmt.Fprintln(&sb, muted.Render("updated "+humanTime(p.UpdatedAt)))
	}
	fmt.Fprintln(&sb, muted.Render(p.WebURL))
	if p.Description != "" {
		fmt.Fprintln(&sb)
		sb.WriteString(p.Description)
	}
	m.detail.SetContent(sb.String())
}

func renderComments(cs []api.Comment) string {
	if len(cs) == 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).
			Render("No comments yet — press n to add one.")
	}
	b := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("13"))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	var sb strings.Builder
	for i, c := range cs {
		if i > 0 {
			sb.WriteString("\n")
		}
		when := ""
		if !c.CreatedAt.IsZero() {
			when = c.CreatedAt.Format("2006-01-02 15:04")
		}
		fmt.Fprintf(&sb, "%s  %s\n", b.Render(c.Author), muted.Render(when))
		sb.WriteString(c.Text)
		sb.WriteString("\n")
	}
	return sb.String()
}

func (m *model) layout() {
	helpHeight := lipgloss.Height(m.helpView())
	listW := m.width / 2
	if listW < 32 {
		listW = m.width
	}
	detailW := m.width - listW - 2
	contentH := m.height - helpHeight - 1

	m.list.SetSize(listW, contentH)
	m.detail.Width = detailW
	m.detail.Height = contentH
	// Diff view always reserves space for the file-tree side panel so
	// split column widths stay stable between toggles.
	m.diff.Width = m.width - m.treeWidth() - 3
	if m.diff.Width < 20 {
		m.diff.Width = m.width
	}
	m.diff.Height = m.height - helpHeight - 1
	m.comments.SetSize(m.width, m.height-helpHeight-2)
}

// treeWidth picks a sensible width for the diff file-tree side panel.
func (m model) treeWidth() int {
	w := m.width / 4
	if w < 24 {
		w = 24
	}
	if w > 40 {
		w = 40
	}
	if w > m.width/2 {
		w = m.width / 2
	}
	if w < 0 {
		w = 0
	}
	return w
}

// renderDiffTree formats the hierarchical file-tree side panel.
// Directories are shown as collapsed-style headings (▾) with files
// indented underneath. The currently-focused row gets a ▶ marker; the
// file containing the diff cursor is highlighted in cyan.
func (m model) renderDiffTree() string {
	w := m.treeWidth()
	h := m.diff.Height
	if h <= 0 {
		h = 10
	}
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")).Width(w).MaxWidth(w)
	cursorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	activeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	dirStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Bold(true)
	rowStyle := lipgloss.NewStyle().Width(w).MaxWidth(w)

	fileCount := 0
	for _, n := range m.diffTree {
		if !n.isDir {
			fileCount++
		}
	}

	var lines []string
	lines = append(lines, titleStyle.Render(fmt.Sprintf("Files (%d)", fileCount)))

	// Active file path so we can colour its row.
	activePath := ""
	for _, f := range m.diffFiles {
		if f.rowIdx <= m.diffCursor {
			activePath = f.path
		}
	}

	for i, node := range m.diffTree {
		indent := strings.Repeat("  ", node.depth)
		var glyph string
		if node.isDir {
			glyph = "▾ "
		} else {
			glyph = "  "
		}
		// Marker takes 2 cells; indent + glyph variable; name fills the rest.
		nameSpace := w - 2 - len(indent) - len(glyph)
		if nameSpace < 1 {
			nameSpace = 1
		}
		name := node.name
		if node.isDir {
			name += "/"
		}
		if len(name) > nameSpace {
			name = name[:nameSpace-1] + "…"
		}

		marker := "  "
		body := indent + glyph + name

		var styled string
		switch {
		case m.diffFocus == "tree" && i == m.diffTreeCursor:
			marker = "▶ "
			styled = cursorStyle.Render(marker + body)
		case !node.isDir && node.path == activePath:
			styled = activeStyle.Render(marker + body)
		case node.isDir:
			styled = dirStyle.Render(marker + body)
		default:
			styled = marker + body
		}
		lines = append(lines, rowStyle.Render(styled))
	}
	// Pad to viewport height so JoinHorizontal doesn't compress the
	// diff content up.
	for len(lines) < h+1 {
		lines = append(lines, strings.Repeat(" ", w))
	}
	if len(lines) > h+1 {
		lines = lines[:h+1]
	}
	return strings.Join(lines, "\n")
}

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
	case viewPalette:
		km = m.keys.paletteHelp()
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
		anchor := ""
		if c, ok := m.activeDiffCell(); ok {
			anchor = fmt.Sprintf("  ·  %s:%d (%s)", c.path, c.line, c.side)
		}
		mode := "unified"
		if m.diffSplit {
			mode = "split"
		}
		overlay := "comments on"
		if !m.diffShowInline {
			overlay = "comments off"
		}
		focus := ""
		if m.diffFocus == "tree" {
			focus = "  ·  [tree]"
		}
		count := ""
		if m.numBuf != "" {
			count = "  ·  ×" + m.numBuf
		}
		header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")).
			Render(fmt.Sprintf("Diff · PR #%d  ·  %s  ·  %s%s%s%s", m.diffID, mode, overlay, anchor, focus, count))
		tree := m.renderDiffTree()
		sep := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).
			Render(strings.Repeat("│\n", lipgloss.Height(tree)))
		split := lipgloss.JoinHorizontal(lipgloss.Top, tree, sep, m.diff.View())
		body = header + "\n" + split
	case viewDetail:
		header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")).Render("PR detail")
		body = header + "\n" + m.detail.View()
	case viewComments:
		header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")).
			Render(fmt.Sprintf("Comments · PR #%d", m.commentsPRID))
		body = header + "\n" + m.comments.View()
	case viewConfirmDelete:
		warn := lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
		body = "\n  " + warn.Render(fmt.Sprintf("Delete comment #%d? (y/n)", m.pendingDeleteCommentID))
	case viewPalette:
		header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")).
			Render("Command palette · type to filter · enter to run · esc to close")
		body = header + "\n" + m.palette.View()
	default:
		left := lipgloss.NewStyle().Padding(0, 1).Render(m.list.View())
		right := lipgloss.NewStyle().Padding(0, 1).Render(m.detail.View())
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	}

	footer := m.helpView()
	statusLine := ""
	if m.loading {
		statusLine = m.spinner.View() + " loading…"
	} else if m.status != "" {
		statusLine = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(m.status)
	}
	if statusLine != "" {
		footer = statusLine + "  " + footer
	}
	return body + "\n" + footer
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

func openInBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler"}
	default:
		cmd = "xdg-open"
	}
	args = append(args, url)
	return exec.Command(cmd, args...).Start()
}

func humanTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func styleState(s string) string {
	switch strings.ToUpper(s) {
	case "OPEN", "INPROGRESS", "PENDING":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(s)
	case "MERGED", "SUCCESSFUL", "SUCCESS":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(s)
	case "DECLINED", "FAILED", "CANCELLED", "STOPPED":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(s)
	}
	return s
}

// Diff styling — package-level so both unified and split renderers
// share the same colour palette without re-allocating per call.
var (
	diffAddStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	diffDelStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	diffHunkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	diffMetaStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("13"))
	diffCtxStyle  = lipgloss.NewStyle()
)

// diffLine is the parsed form of one line of a unified diff. We hold
// the raw text and style separately so we can re-render it at any
// width when laying out a split view.
type diffLine struct {
	raw       string
	style     lipgloss.Style
	path      string
	side      string // "new", "old", "" (header / hunk / unknown), or "both" (context)
	lineNo    int    // file line number on the new side (or sole side)
	oldNo     int    // for "both" rows, the line on the old side
	preferOld bool   // for "both" rows, anchor on the old side
}

func (d diffLine) styled() string { return d.style.Render(d.raw) }

// commentable reports whether this row can carry an inline comment.
func (d diffLine) commentable() bool { return d.path != "" && d.side != "" && d.lineNo > 0 }

// cleanDiffPath strips the various prefix conventions that show up in
// unified diff headers so the result is a plain repo-relative path
// matching what inline-comment anchors use. Bitbucket Server emits
// `src://` and `dst://` for some diffs in addition to git's `a/`/`b/`.
func cleanDiffPath(s string) string {
	s = strings.TrimSpace(s)
	for _, p := range []string{"src://", "dst://", "a/", "b/"} {
		if strings.HasPrefix(s, p) {
			s = strings.TrimPrefix(s, p)
			break
		}
	}
	return s
}

// parseDiff walks a unified diff and produces one diffLine per text
// line, decorated with file/line/side metadata where applicable.
func parseDiff(diff string) []diffLine {
	var out []diffLine
	var path string
	var oldLine, newLine int

	for _, line := range strings.Split(diff, "\n") {
		dl := diffLine{raw: line, style: diffCtxStyle}
		switch {
		case strings.HasPrefix(line, "diff "):
			// "diff --git a/foo b/foo" — take the b/ side as the post-image path.
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				path = cleanDiffPath(parts[3])
			}
			dl.style = diffMetaStyle
		case strings.HasPrefix(line, "+++ "):
			p := cleanDiffPath(strings.TrimPrefix(line, "+++ "))
			if p != "/dev/null" {
				path = p
			}
			dl.style = diffMetaStyle
		case strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "index "):
			dl.style = diffMetaStyle
		case strings.HasPrefix(line, "@@"):
			oldLine, newLine = parseHunkHeader(line)
			dl.style = diffHunkStyle
		case strings.HasPrefix(line, "+"):
			dl.style = diffAddStyle
			dl.path, dl.side, dl.lineNo = path, "new", newLine
			newLine++
		case strings.HasPrefix(line, "-"):
			dl.style = diffDelStyle
			dl.path, dl.side, dl.lineNo = path, "old", oldLine
			oldLine++
		case strings.HasPrefix(line, " "):
			// Context: exists on both sides. We default to "new" for
			// commenting; user can press 't' to flip.
			dl.path, dl.side, dl.lineNo, dl.oldNo = path, "both", newLine, oldLine
			oldLine++
			newLine++
		case strings.HasPrefix(line, `\`):
			// "\ No newline at end of file" — leave plain.
		}
		out = append(out, dl)
	}
	return out
}

// parseHunkHeader extracts the starting line numbers from "@@ -A,B +C,D @@".
func parseHunkHeader(line string) (oldStart, newStart int) {
	rest := strings.TrimPrefix(line, "@@")
	rest = strings.TrimSpace(rest)
	fields := strings.Fields(rest)
	if len(fields) < 2 {
		return 0, 0
	}
	parseRange := func(s string) int {
		s = strings.TrimPrefix(s, "-")
		s = strings.TrimPrefix(s, "+")
		if i := strings.Index(s, ","); i >= 0 {
			s = s[:i]
		}
		n, _ := strconv.Atoi(s)
		return n
	}
	return parseRange(fields[0]), parseRange(fields[1])
}

// diffCell is one column of one display row. Cells carry both visual
// content and the (path, side, line) anchor needed when the user fires
// 'c' to add an inline comment from this position.
type diffCell struct {
	raw   string
	style lipgloss.Style
	path  string
	side  string
	line  int
}

func (c diffCell) commentable() bool { return c.path != "" && c.side != "" && c.line > 0 }

// diffRow is one logical row in the rendered diff. In unified mode
// only cells[0] is populated; in split mode cells[0] is the old (LHS)
// side and cells[1] is the new (RHS) side. Hunk and file headers use
// fullWidth=true to span both columns.
type diffRow struct {
	cells      [2]diffCell
	fullWidth  bool
	annotation bool           // comment annotation row (no anchor of its own)
	annoText   string         // plain text (no ANSI) for annotation
	annoStyle  lipgloss.Style // style applied at render time at the right width
	annoSide   int            // 0 = old column, 1 = new column (split only)
}

// rebuildDiffRows regenerates the display row sequence from the parsed
// diff lines + comments according to the current toggles. Called after
// the diff loads, the comments load, the user toggles split / inline,
// or the viewport resizes.
func (m *model) rebuildDiffRows() {
	if m.diffSplit {
		m.diffRows = buildSplitRows(m.diffLines)
	} else {
		m.diffRows = buildUnifiedRows(m.diffLines)
	}
	if m.diffShowInline {
		m.diffRows = injectInlineComments(m.diffRows, m.diffComments)
	}
	m.diffFiles = computeDiffFiles(m.diffRows)
	m.diffTree = computeFileTree(m.diffFiles)
	m.diff.SetContent(m.renderDiffRows())
	m.clampDiffCursor()
	m.syncTreeCursor()
}

// computeDiffFiles scans the rendered rows and records the position of
// each file's "diff --git" header so the tree can jump-scroll there.
func computeDiffFiles(rows []diffRow) []diffFile {
	var out []diffFile
	for i, r := range rows {
		if !r.fullWidth {
			continue
		}
		raw := r.cells[0].raw
		if !strings.HasPrefix(raw, "diff ") {
			continue
		}
		parts := strings.Fields(raw)
		if len(parts) < 4 {
			continue
		}
		out = append(out, diffFile{path: cleanDiffPath(parts[3]), rowIdx: i})
	}
	return out
}

// syncTreeCursor moves the tree cursor to whichever file the diff
// cursor currently sits inside.
func (m *model) syncTreeCursor() {
	// Find the file containing the diff cursor.
	activePath := ""
	for _, f := range m.diffFiles {
		if f.rowIdx <= m.diffCursor {
			activePath = f.path
		}
	}
	if activePath == "" {
		return
	}
	for i, n := range m.diffTree {
		if !n.isDir && n.path == activePath {
			m.diffTreeCursor = i
			return
		}
	}
}

func buildUnifiedRows(lines []diffLine) []diffRow {
	rows := make([]diffRow, 0, len(lines))
	for _, dl := range lines {
		c := diffCell{raw: dl.raw, style: dl.style}
		if dl.commentable() {
			side, lineNo := inlineSide(dl)
			c.path, c.side, c.line = dl.path, side, lineNo
		}
		// Header / hunk rows have no side; mark as fullWidth so the
		// file-tree builder can locate file boundaries (and so future
		// styling can treat them specially even in unified mode).
		full := dl.side == ""
		rows = append(rows, diffRow{cells: [2]diffCell{c}, fullWidth: full})
	}
	return rows
}

// buildSplitRows pairs runs of "-" with "+" so removed and added text
// sit on opposite columns of the same display row. Context lines
// appear identically on both sides; headers / hunk markers use
// fullWidth so they span the full width of the viewport.
func buildSplitRows(lines []diffLine) []diffRow {
	rows := []diffRow{}
	emptyOld := diffCell{}
	emptyNew := diffCell{}
	for i := 0; i < len(lines); {
		dl := lines[i]
		switch dl.side {
		case "":
			rows = append(rows, diffRow{cells: [2]diffCell{{raw: dl.raw, style: dl.style}}, fullWidth: true})
			i++
		case "both":
			body := strings.TrimPrefix(dl.raw, " ")
			oldCell := diffCell{raw: " " + body, style: diffCtxStyle, path: dl.path, side: "old", line: dl.oldNo}
			newCell := diffCell{raw: " " + body, style: diffCtxStyle, path: dl.path, side: "new", line: dl.lineNo}
			rows = append(rows, diffRow{cells: [2]diffCell{oldCell, newCell}})
			i++
		case "old":
			dels := []diffLine{dl}
			i++
			for i < len(lines) && lines[i].side == "old" {
				dels = append(dels, lines[i])
				i++
			}
			adds := []diffLine{}
			for i < len(lines) && lines[i].side == "new" {
				adds = append(adds, lines[i])
				i++
			}
			n := len(dels)
			if len(adds) > n {
				n = len(adds)
			}
			for j := 0; j < n; j++ {
				row := diffRow{cells: [2]diffCell{emptyOld, emptyNew}}
				if j < len(dels) {
					d := dels[j]
					row.cells[0] = diffCell{raw: d.raw, style: d.style, path: d.path, side: "old", line: d.lineNo}
				}
				if j < len(adds) {
					a := adds[j]
					row.cells[1] = diffCell{raw: a.raw, style: a.style, path: a.path, side: "new", line: a.lineNo}
				}
				rows = append(rows, row)
			}
		case "new":
			adds := []diffLine{dl}
			i++
			for i < len(lines) && lines[i].side == "new" {
				adds = append(adds, lines[i])
				i++
			}
			for _, a := range adds {
				rows = append(rows, diffRow{cells: [2]diffCell{
					emptyOld,
					{raw: a.raw, style: a.style, path: a.path, side: "new", line: a.lineNo},
				}})
			}
		default:
			i++
		}
	}
	return rows
}

// injectInlineComments walks the row list and inserts an annotation
// row (or two, for split) immediately after each row that has a
// matching inline comment anchor.
func injectInlineComments(rows []diffRow, comments []api.Comment) []diffRow {
	if len(comments) == 0 {
		return rows
	}
	// Index comments by (path, side, line) → []Comment so we can
	// attach multiple replies to the same anchor.
	type key struct {
		path string
		side string
		line int
	}
	byAnchor := map[key][]api.Comment{}
	for _, c := range comments {
		if c.Inline == nil {
			continue
		}
		k := key{c.Inline.Path, c.Inline.Side, c.Inline.Line}
		byAnchor[k] = append(byAnchor[k], c)
	}
	if len(byAnchor) == 0 {
		return rows
	}
	out := make([]diffRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, row)
		if row.fullWidth {
			continue
		}
		for idx, cell := range row.cells {
			if !cell.commentable() {
				continue
			}
			cs := byAnchor[key{cell.path, cell.side, cell.line}]
			for _, c := range cs {
				text, style := formatInlineAnnotation(c)
				out = append(out, diffRow{
					annotation: true,
					annoText:   text,
					annoStyle:  style,
					annoSide:   idx,
				})
			}
		}
	}
	return out
}

// formatInlineAnnotation returns the plain (no-ANSI) text plus the
// style to apply at render time. Keeping these separate lets us
// truncate or wrap the text correctly per layout (split column width
// vs full unified width) without garbling embedded escape codes.
func formatInlineAnnotation(c api.Comment) (string, lipgloss.Style) {
	body := strings.TrimSpace(c.Text)
	if body == "" {
		body = "(empty)"
	}
	first := strings.SplitN(body, "\n", 2)[0]
	if len(first) > 500 {
		first = first[:497] + "…"
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	return "💬 " + c.Author + ": " + first, style
}

// renderDiffRows produces the final string fed to the viewport.
// For split rows it formats two columns; for unified one. The cursor
// row gets a "▶" marker at the start.
func (m *model) renderDiffRows() string {
	var b strings.Builder
	pointer := lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true).Render("▶ ")
	leftPad := "  "

	width := m.diff.Width
	if width <= 0 {
		width = 80
	}
	colW := (width - 5) / 2 // 2 for marker, 3 for separator " │ "
	if colW < 10 {
		colW = 10
	}

	for i, row := range m.diffRows {
		marker := leftPad
		if i == m.diffCursor {
			marker = pointer
		}
		switch {
		case row.annotation:
			b.WriteString(leftPad)
			if m.diffSplit {
				cell := row.annoStyle.Width(colW).MaxWidth(colW).Render(truncateForCell(row.annoText, colW))
				blank := strings.Repeat(" ", colW)
				if row.annoSide == 0 {
					b.WriteString(cell)
					b.WriteString(" │ ")
					b.WriteString(blank)
				} else {
					b.WriteString(blank)
					b.WriteString(" │ ")
					b.WriteString(cell)
				}
			} else {
				b.WriteString("    " + row.annoStyle.Render(row.annoText))
			}
		case row.fullWidth:
			b.WriteString(marker)
			b.WriteString(row.cells[0].style.Render(row.cells[0].raw))
		case m.diffSplit:
			b.WriteString(marker)
			b.WriteString(renderSplitCell(row.cells[0], colW, i == m.diffCursor && m.diffCursorSide == 0))
			b.WriteString(" │ ")
			b.WriteString(renderSplitCell(row.cells[1], colW, i == m.diffCursor && m.diffCursorSide == 1))
		default:
			b.WriteString(marker)
			b.WriteString(row.cells[0].style.Render(row.cells[0].raw))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// renderSplitCell renders one cell of a split row, padding/truncating
// to width. The active-side flag draws an underline so the user can
// see which column 'c' will target.
func renderSplitCell(c diffCell, w int, active bool) string {
	style := c.style.Width(w).MaxWidth(w)
	if active {
		style = style.Underline(true)
	}
	if c.raw == "" {
		return strings.Repeat(" ", w)
	}
	return style.Render(c.raw)
}

// truncateForCell trims plain text to fit a column of width w in
// terminal cells (using lipgloss.Width which is ANSI- and rune-aware).
// We trim from the end and append a single-cell ellipsis.
func truncateForCell(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	runes := []rune(s)
	for n := len(runes) - 1; n > 0; n-- {
		cut := string(runes[:n]) + "…"
		if lipgloss.Width(cut) <= w {
			return cut
		}
	}
	return "…"
}

// clampDiffCursor keeps the cursor within bounds after the row list
// is rebuilt.
func (m *model) clampDiffCursor() {
	if len(m.diffRows) == 0 {
		m.diffCursor = 0
		return
	}
	if m.diffCursor < 0 {
		m.diffCursor = 0
	}
	if m.diffCursor >= len(m.diffRows) {
		m.diffCursor = len(m.diffRows) - 1
	}
}

// activeDiffCell returns the cell the cursor is currently pointing to,
// honouring the column selection in split mode.
func (m *model) activeDiffCell() (diffCell, bool) {
	if m.diffCursor >= len(m.diffRows) {
		return diffCell{}, false
	}
	row := m.diffRows[m.diffCursor]
	if row.fullWidth || row.annotation {
		return diffCell{}, false
	}
	idx := 0
	if m.diffSplit {
		idx = m.diffCursorSide
	}
	c := row.cells[idx]
	if !c.commentable() {
		// Fall back to the other side if active side is empty (e.g.
		// pure addition row in split, cursor on old side).
		other := row.cells[1-idx]
		if other.commentable() {
			return other, true
		}
	}
	return c, c.commentable()
}

// ensureDiffCursorVisible scrolls the viewport if the cursor would
// otherwise be off-screen.
func (m *model) ensureDiffCursorVisible() {
	h := m.diff.Height
	if h <= 0 {
		return
	}
	top := m.diff.YOffset
	bot := top + h - 1
	switch {
	case m.diffCursor < top:
		m.diff.SetYOffset(m.diffCursor)
	case m.diffCursor > bot:
		m.diff.SetYOffset(m.diffCursor - h + 1)
	}
}

// inlineSide normalises a row's side into the API value. "both" rows
// (context) default to "new"; flipping dl.preferOld switches to "old".
func inlineSide(dl diffLine) (string, int) {
	if dl.side == "both" {
		if dl.preferOld {
			return "old", dl.oldNo
		}
		return "new", dl.lineNo
	}
	return dl.side, dl.lineNo
}

// editInlineInTUI is editInTUI's cousin for line-anchored comments —
// it carries path/line/side through to the result message.
func editInlineInTUI(prID int, path string, lineNo int, side string) tea.Cmd {
	hint := fmt.Sprintf("pr-%d-inline-L%d-%s", prID, lineNo, side)
	f, err := os.CreateTemp("", "bb-edit-*-"+sanitizeForFilename(hint)+".md")
	if err != nil {
		return func() tea.Msg {
			return editorResultMsg{purpose: "add-inline-comment", prID: prID, path: path, line: lineNo, side: side, err: err}
		}
	}
	tmp := f.Name()
	header := fmt.Sprintf("<!-- inline comment on %s:%d (%s side) -->\n", path, lineNo, side)
	_, _ = f.WriteString(header)
	f.Close()

	editor := config.Get().EditorCmd()
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		os.Remove(tmp)
		return func() tea.Msg {
			return editorResultMsg{purpose: "add-inline-comment", prID: prID, path: path, line: lineNo, side: side, err: fmt.Errorf("no editor configured")}
		}
	}
	args := append(parts[1:], tmp)
	cmd := exec.Command(parts[0], args...)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		defer os.Remove(tmp)
		if err != nil {
			return editorResultMsg{purpose: "add-inline-comment", prID: prID, path: path, line: lineNo, side: side, err: err}
		}
		data, rerr := os.ReadFile(tmp)
		// Strip the comment-marker header we wrote so the user can keep it.
		text := string(data)
		text = strings.TrimPrefix(text, header)
		return editorResultMsg{purpose: "add-inline-comment", prID: prID, path: path, line: lineNo, side: side, text: text, err: rerr}
	})
}

// sanitizeForFilename replaces path separators so the temp filename
// stays in /tmp without unintended subdirectories.
func sanitizeForFilename(s string) string {
	r := strings.NewReplacer("/", "-", "\\", "-", " ", "_")
	return r.Replace(s)
}

// (colorizeDiff removed — parseDiff produces both metadata and styled
// text in one pass.)
