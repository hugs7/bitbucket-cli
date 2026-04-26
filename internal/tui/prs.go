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

	"github.com/hugs7/bitbucket-cli/internal/api"
	"github.com/hugs7/bitbucket-cli/internal/config"
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
		short: [][]key.Binding{{k.Up, k.Down, k.TreeFocus, k.NextFile, k.InlineComment, k.ReplyComment, k.ToggleSplit, k.ToggleInline, k.Back, k.Quit}},
		full: [][]key.Binding{
			{k.Up, k.Down, k.InlineComment, k.ReplyComment, k.ToggleSide},
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
		short: [][]key.Binding{{k.Up, k.Down, k.Enter, k.AddComment, k.ReplyComment, k.EditComment, k.DeleteComment, k.Back, k.Quit}},
		full: [][]key.Binding{
			{k.Up, k.Down, k.Enter, k.AddComment, k.ReplyComment},
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

	// diffPendingJump, when set, tells the diffLoadedMsg handler to
	// place the cursor on the row matching this anchor instead of the
	// first commentable row. Used for "jump from comments view".
	diffPendingJump *diffJumpTarget

	// Vim-style count prefix accumulator (e.g. "15j"). Consumed by the
	// next motion key and reset.
	numBuf string

	// zPending is set after the user presses 'z' in diff view; the
	// next keypress (z/t/b) completes a vim z-motion (center / top /
	// bottom). Reset on any other key.
	zPending bool

	// treeReturnCursor stores the diff cursor position at the moment
	// the user focused the file tree, so esc inside the tree can
	// restore it (cancel the preview navigation).
	treeReturnCursor int
	treeReturnSet    bool

	// Vim-style jump list. jumpBack is a stack of past cursor
	// positions; jumpFwd is the redo stack populated when the user
	// goes back via ctrl-o so they can ctrl-i forward again.
	jumpBack []int
	jumpFwd  []int

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

// diffJumpTarget identifies a specific anchor (path/side/line) the
// diff cursor should be placed on after the next diff load. This is
// used when entering the diff view from a comment thread.
type diffJumpTarget struct {
	path string
	side string
	line int
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
// isOwnPR reports whether the given PR was opened by the configured
// user. Bitbucket rejects self-approval, so callers gate the
// Approve / Unapprove / NeedsWork commands on this to avoid an API
// round-trip that always fails.
func (m *model) isOwnPR(pr api.PullRequest) bool {
	me := strings.ToLower(strings.TrimSpace(m.svc.Me()))
	if me == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(pr.Author), me)
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
		computeWordHighlights(m.diffLines)
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
		// If a jump target was queued (e.g. from "Enter on a comment"),
		// place the cursor on the matching row. Otherwise fall back to
		// the first commentable row in the diff.
		jumped := false
		if m.diffPendingJump != nil {
			t := *m.diffPendingJump
			m.diffPendingJump = nil
			for i, r := range m.diffRows {
				if r.fullWidth || r.annotation {
					continue
				}
				for _, c := range r.cells {
					if c.commentable() && c.path == t.path && c.side == t.side && c.line == t.line {
						m.diffCursor = i
						jumped = true
						break
					}
				}
				if jumped {
					break
				}
			}
		}
		if !jumped {
			for i, r := range m.diffRows {
				if r.fullWidth || r.annotation {
					continue
				}
				if r.cells[0].commentable() || r.cells[1].commentable() {
					m.diffCursor = i
					break
				}
			}
		}
		m.syncTreeCursor()
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
		case "reply-inline-comment":
			// Same as reply-comment, but stay in the diff view: post,
			// then refresh the diff's inline-comment overlay instead
			// of swapping into the comments list.
			parent := msg.commentID
			prID := msg.prID
			label := fmt.Sprintf("replied to #%d", parent)
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, func() tea.Msg {
				if _, err := m.svc.ReplyComment(m.project, m.slug, prID, parent, text); err != nil {
					return actionDoneMsg{text: label, err: err}
				}
				cs, lerr := m.svc.ListComments(m.project, m.slug, prID)
				if lerr != nil {
					return actionDoneMsg{text: label + " (overlay reload failed)", err: lerr}
				}
				return diffCommentsLoadedMsg{id: prID, comments: cs}
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
			// Esc inside the diff file-tree returns focus to the diff
			// AND restores the cursor to where it was when the user
			// entered tree mode — so a "preview navigation" can be
			// abandoned cleanly.
			if m.mode == viewDiff && m.diffFocus == "tree" {
				m.diffFocus = "diff"
				if m.treeReturnSet {
					m.diffCursor = m.treeReturnCursor
					m.treeReturnSet = false
					m.syncTreeCursor()
					m.diff.SetContent(m.renderDiffRows())
					m.ensureDiffCursorVisible()
				}
				return m, nil
			}
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

			// Complete a pending vim z-motion (zz / zt / zb).
			if m.zPending {
				m.zPending = false
				m.status = ""
				switch s {
				case "z":
					off := m.diffCursor - m.diff.Height/2
					if off < 0 {
						off = 0
					}
					m.diff.SetYOffset(off)
				case "t":
					m.diff.SetYOffset(m.diffCursor)
				case "b":
					off := m.diffCursor - m.diff.Height + 1
					if off < 0 {
						off = 0
					}
					m.diff.SetYOffset(off)
				}
				return m, nil
			}

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
				// previewFile updates the diff content to show the
				// file under the tree cursor without leaving tree
				// focus — so j/k browsing acts like a live preview.
				previewFile := func() {
					if m.diffTreeCursor >= len(m.diffTree) {
						return
					}
					node := m.diffTree[m.diffTreeCursor]
					if node.isDir {
						return
					}
					m.diffCursor = node.rowIdx
					m.diff.SetContent(m.renderDiffRows())
					m.ensureDiffCursorVisible()
				}
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
					previewFile()
				}
				switch {
				case key.Matches(msg, m.keys.Up):
					moveTree(-count)
					return m, nil
				case key.Matches(msg, m.keys.Down):
					moveTree(count)
					return m, nil
				case key.Matches(msg, m.keys.TreeSelect), s == "l":
					// Commit: stay where the preview put us and
					// abandon the saved return-cursor.
					m.diffFocus = "diff"
					m.treeReturnSet = false
					m.diff.SetContent(m.renderDiffRows())
					m.ensureDiffCursorVisible()
					return m, nil
				case key.Matches(msg, m.keys.TreeFocus):
					// Tab toggles back to diff focus AND keeps the
					// preview position (commits, like enter).
					m.diffFocus = "diff"
					m.treeReturnSet = false
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
			// pushJump remembers the current cursor on the back stack
			// (vim ''  / ctrl-o), clearing the forward stack since we
			// just made a new authoritative move.
			pushJump := func() {
				m.jumpBack = append(m.jumpBack, m.diffCursor)
				if len(m.jumpBack) > 100 {
					m.jumpBack = m.jumpBack[len(m.jumpBack)-100:]
				}
				m.jumpFwd = nil
			}
			jumpTo := func(target int) {
				if target < 0 {
					target = 0
				}
				if target > n-1 {
					target = n - 1
				}
				pushJump()
				m.diffCursor = target
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
			case s == "ctrl+d", s == "d":
				step := m.diff.Height / 2
				if step < 1 {
					step = 1
				}
				move(step * count)
				return m, nil
			case s == "ctrl+u", s == "u":
				step := m.diff.Height / 2
				if step < 1 {
					step = 1
				}
				move(-step * count)
				return m, nil
			case s == "ctrl+o":
				// Pop the back stack: jump to the most recent saved
				// position, pushing current cursor onto the forward
				// stack so ctrl-i can return.
				if len(m.jumpBack) == 0 {
					m.status = "jump list empty"
					return m, nil
				}
				prev := m.jumpBack[len(m.jumpBack)-1]
				m.jumpBack = m.jumpBack[:len(m.jumpBack)-1]
				m.jumpFwd = append(m.jumpFwd, m.diffCursor)
				m.diffCursor = prev
				if m.diffCursor < 0 {
					m.diffCursor = 0
				}
				if m.diffCursor > n-1 {
					m.diffCursor = n - 1
				}
				m.syncTreeCursor()
				m.diff.SetContent(m.renderDiffRows())
				m.ensureDiffCursorVisible()
				return m, nil
			case s == "ctrl+i":
				// ctrl-i reverses ctrl-o. (Many terminals send the
				// same byte for Tab and Ctrl-I; bubbletea normalises
				// Tab to "tab" and only "real" ctrl-i to "ctrl+i",
				// so we don't need to disambiguate here.)
				if len(m.jumpFwd) == 0 {
					m.status = "no forward jumps"
					return m, nil
				}
				next := m.jumpFwd[len(m.jumpFwd)-1]
				m.jumpFwd = m.jumpFwd[:len(m.jumpFwd)-1]
				m.jumpBack = append(m.jumpBack, m.diffCursor)
				m.diffCursor = next
				if m.diffCursor < 0 {
					m.diffCursor = 0
				}
				if m.diffCursor > n-1 {
					m.diffCursor = n - 1
				}
				m.syncTreeCursor()
				m.diff.SetContent(m.renderDiffRows())
				m.ensureDiffCursorVisible()
				return m, nil
			case s == "g":
				jumpTo(0)
				return m, nil
			case s == "G":
				jumpTo(n - 1)
				return m, nil
			case s == "ctrl+e":
				// Scroll viewport down one line (cursor follows so it
				// stays inside the visible window).
				m.diff.SetYOffset(m.diff.YOffset + count)
				if m.diffCursor < m.diff.YOffset {
					m.diffCursor = m.diff.YOffset
					m.syncTreeCursor()
					m.diff.SetContent(m.renderDiffRows())
				}
				return m, nil
			case s == "ctrl+y":
				m.diff.SetYOffset(m.diff.YOffset - count)
				bottom := m.diff.YOffset + m.diff.Height - 1
				if m.diffCursor > bottom {
					m.diffCursor = bottom
					if m.diffCursor < 0 {
						m.diffCursor = 0
					}
					m.syncTreeCursor()
					m.diff.SetContent(m.renderDiffRows())
				}
				return m, nil
			case s == "z":
				// Two-key z-motions: zz = center, zt = cursor to top,
				// zb = cursor to bottom of the visible viewport. The
				// next keypress is intercepted by the zPending check
				// at the top of the diff key handler.
				m.numBuf = ""
				m.zPending = true
				m.status = "z-"
				return m, nil
			case s == "H":
				m.diffCursor = m.diff.YOffset
				m.syncTreeCursor()
				m.diff.SetContent(m.renderDiffRows())
				return m, nil
			case s == "M":
				m.diffCursor = m.diff.YOffset + m.diff.Height/2
				if m.diffCursor >= n {
					m.diffCursor = n - 1
				}
				m.syncTreeCursor()
				m.diff.SetContent(m.renderDiffRows())
				return m, nil
			case s == "L":
				m.diffCursor = m.diff.YOffset + m.diff.Height - 1
				if m.diffCursor >= n {
					m.diffCursor = n - 1
				}
				m.syncTreeCursor()
				m.diff.SetContent(m.renderDiffRows())
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
					jumpTo(m.diffFiles[idx].rowIdx)
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
					jumpTo(m.diffFiles[idx].rowIdx)
				}
				return m, nil
			case key.Matches(msg, m.keys.TreeFocus):
				if len(m.diffFiles) == 0 {
					m.status = "no files in diff"
					return m, nil
				}
				// Remember where we were in the diff so esc inside
				// the tree can roll the preview back.
				m.treeReturnCursor = m.diffCursor
				m.treeReturnSet = true
				m.syncTreeCursor()
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
			case key.Matches(msg, m.keys.ReplyComment):
				c, ok := m.activeDiffCell()
				if !ok {
					m.status = "no file/line under cursor"
					return m, nil
				}
				// Reply to the latest comment threaded at this anchor.
				parent := 0
				for _, cm := range m.diffComments {
					if cm.Inline != nil &&
						cm.Inline.Path == c.path &&
						cm.Inline.Side == c.side &&
						cm.Inline.Line == c.line {
						parent = cm.ID
					}
				}
				if parent == 0 {
					m.status = "no comment here to reply to — press c to add one"
					return m, nil
				}
				return m, editInTUI("reply-inline-comment",
					fmt.Sprintf("pr-%d-reply-to-%d", m.diffID, parent),
					m.diffID, parent, "")
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
					if m.isOwnPR(it.pr) {
						m.status = "✗ can't approve your own PR"
						return m, nil
					}
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
					if m.isOwnPR(it.pr) {
						m.status = "✗ can't mark your own PR as needs work"
						return m, nil
					}
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
			case key.Matches(msg, m.keys.Enter):
				// Jump to the diff at the comment's anchor (if inline).
				if it, ok := m.comments.SelectedItem().(commentItem); ok {
					if it.c.Inline == nil {
						m.status = "no inline anchor — comment is PR-level"
						return m, nil
					}
					m.diffPendingJump = &diffJumpTarget{
						path: it.c.Inline.Path,
						side: it.c.Inline.Side,
						line: it.c.Inline.Line,
					}
					m.loading = true
					return m, tea.Batch(m.spinner.Tick, m.fetchDiff(m.commentsPRID))
				}
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
				if m.isOwnPR(it.pr) {
					m.status = "✗ can't approve your own PR"
					return m, nil
				}
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
				if m.isOwnPR(it.pr) {
					m.status = "✗ can't mark your own PR as needs work"
					return m, nil
				}
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
		mode := titleChip.Render("unified")
		if m.diffSplit {
			mode = titleChip.Render("split")
		}
		overlay := titleChipDim.Render("comments off")
		if m.diffShowInline {
			overlay = titleChip.Render("comments on")
		}
		anchor := ""
		if c, ok := m.activeDiffCell(); ok {
			anchor = titleChipDim.Render(fmt.Sprintf("%s:%d (%s)", c.path, c.line, c.side))
		}
		focus := ""
		if m.diffFocus == "tree" {
			focus = titleChipWarn.Render("[tree]")
		}
		count := ""
		if m.numBuf != "" {
			count = titleChipWarn.Render("×" + m.numBuf)
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
			titleChipDim.Render(fmt.Sprintf("%d total", len(m.commentsList)))) + "\n" + m.comments.View()
	case viewConfirmDelete:
		warn := lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")).
			Bold(true).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("9")).
			Padding(0, 2)
		body = "\n  " + warn.Render(fmt.Sprintf("Delete comment #%d?  [y/n]", m.pendingDeleteCommentID))
	case viewPalette:
		header := titleBar("COMMAND PALETTE",
			titleChipDim.Render("type to filter"),
			titleChipDim.Render("enter runs · esc closes"))
		body = header + "\n" + m.palette.View()
	default:
		header := titleBar(fmt.Sprintf("PULL REQUESTS · %s/%s", m.project, m.slug),
			titleChip.Render(strings.ToUpper(m.state)),
			titleChipDim.Render(fmt.Sprintf("%d shown", len(m.list.Items()))))
		left := lipgloss.NewStyle().Padding(0, 1).Render(m.list.View())
		right := lipgloss.NewStyle().Padding(0, 1).Render(m.detail.View())
		body = header + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	}

	footer := m.helpView()
	statusLine := ""
	if m.loading {
		statusLine = statusInfo.Render(m.spinner.View() + " loading…")
	} else if m.status != "" {
		switch {
		case strings.HasPrefix(m.status, "✗"):
			statusLine = statusErr.Render(m.status)
		case strings.HasPrefix(m.status, "✓"):
			statusLine = statusOK.Render(m.status)
		default:
			statusLine = statusInfo.Render(m.status)
		}
	}
	if statusLine != "" {
		footer = statusLine + "  " + titleSep + "  " + footer
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
	// Bitbucket-style "shading": added lines get a dark-green wash,
	// removed lines a dark-red wash, context stays neutral. The
	// foreground colours are picked to stay readable on those
	// backgrounds in both 256-colour and truecolor terminals.
	diffAddStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("194")).Background(lipgloss.Color("22"))
	diffDelStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("224")).Background(lipgloss.Color("52"))
	diffHunkStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("159")).Background(lipgloss.Color("24"))
	diffMetaStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(lipgloss.Color("54"))
	diffCtxStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))

	// Inline-comment annotation styling. Comment blocks get a
	// distinctive purple/indigo wash so they read as overlays
	// floating on top of the diff. The header line is brighter and
	// bold so the eye locks onto each new comment quickly.
	annoHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(lipgloss.Color("57"))
	annoBodyStyle   = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("254")).Background(lipgloss.Color("237"))
	annoGutterStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Background(lipgloss.Color("57"))
	annoBodyGutter  = lipgloss.NewStyle().Foreground(lipgloss.Color("141")).Background(lipgloss.Color("237"))

	// Title bar / chip styling — used to give each view a consistent
	// "header chrome" with a coloured pill, dim separators, and
	// muted secondary chips for context (mode, file, focus, ×count).
	titleBarPad = lipgloss.NewStyle().Padding(0, 1)
	// Bright white on deep indigo — high contrast on any theme.
	// (color "0" + bg "12" rendered as illegible grey-on-blue on many
	// 256-colour palettes.)
	titleBadge    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(lipgloss.Color("57")).Padding(0, 1)
	titleAccent   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("141"))
	titleSep      = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(" • ")
	titleChip     = lipgloss.NewStyle().Foreground(lipgloss.Color("159"))
	titleChipDim  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	titleChipWarn = lipgloss.NewStyle().Foreground(lipgloss.Color("221"))
	statusOK      = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	statusErr     = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	statusInfo    = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
)

// titleBar composes a uniform header line: a coloured badge with the
// section name, followed by optional context "chips" separated by dim
// bullets. Empty chips are skipped so callers can pass conditional
// strings without worrying about double-separators.
func titleBar(section string, chips ...string) string {
	parts := []string{titleBadge.Render(section)}
	for _, c := range chips {
		if strings.TrimSpace(c) == "" {
			continue
		}
		parts = append(parts, titleSep, c)
	}
	return titleBarPad.Render(strings.Join(parts, ""))
}

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
	// highlights are byte ranges within raw that should be painted in
	// a brighter "word-diff" colour to draw attention to the actual
	// edited tokens within an otherwise-shaded line.
	highlights []hlRange
}

// hlRange is a [start, end) byte span within a diffLine.raw or
// diffCell.raw used to overlay word-diff highlights.
type hlRange struct{ start, end int }

// Word-diff overlay colours: brighter shades of the line backgrounds
// so the changed tokens within an added/removed line read as the
// "edit" while the surrounding text reads as "context for the edit".
var (
	diffAddHL = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(lipgloss.Color("28"))
	diffDelHL = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(lipgloss.Color("124"))
)

// tokenizeForDiff splits s into a stream of tokens — runs of word
// characters and individual non-word characters. Stable byte offsets
// let us map LCS results back to ranges in the original string.
func tokenizeForDiff(s string) []string {
	var out []string
	i := 0
	for i < len(s) {
		c := s[i]
		isWord := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
		if isWord {
			j := i + 1
			for j < len(s) {
				d := s[j]
				if !((d >= 'a' && d <= 'z') || (d >= 'A' && d <= 'Z') || (d >= '0' && d <= '9') || d == '_') {
					break
				}
				j++
			}
			out = append(out, s[i:j])
			i = j
		} else {
			out = append(out, s[i:i+1])
			i++
		}
	}
	return out
}

// wordDiffRanges computes the byte ranges within a and b that differ
// by tokens (LCS-based). Ignored when either side is empty.
func wordDiffRanges(a, b string) (aRanges, bRanges []hlRange) {
	if a == "" || b == "" {
		return nil, nil
	}
	ta := tokenizeForDiff(a)
	tb := tokenizeForDiff(b)
	n, m := len(ta), len(tb)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if ta[i-1] == tb[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	sameA := make([]bool, n)
	sameB := make([]bool, m)
	i, j := n, m
	for i > 0 && j > 0 {
		switch {
		case ta[i-1] == tb[j-1]:
			sameA[i-1] = true
			sameB[j-1] = true
			i--
			j--
		case dp[i-1][j] >= dp[i][j-1]:
			i--
		default:
			j--
		}
	}
	pos := 0
	for k, t := range ta {
		if !sameA[k] {
			aRanges = append(aRanges, hlRange{pos, pos + len(t)})
		}
		pos += len(t)
	}
	pos = 0
	for k, t := range tb {
		if !sameB[k] {
			bRanges = append(bRanges, hlRange{pos, pos + len(t)})
		}
		pos += len(t)
	}
	return coalesceHL(aRanges), coalesceHL(bRanges)
}

func coalesceHL(rs []hlRange) []hlRange {
	if len(rs) <= 1 {
		return rs
	}
	out := []hlRange{rs[0]}
	for _, r := range rs[1:] {
		last := &out[len(out)-1]
		if r.start <= last.end {
			if r.end > last.end {
				last.end = r.end
			}
		} else {
			out = append(out, r)
		}
	}
	return out
}

// computeWordHighlights walks parsed diff lines, finds runs of "-"
// followed by "+" (the typical hunk shape), and pair-wise computes
// word-diff ranges into each line's .highlights field. Ranges include
// the leading "+"/"-" prefix offset so renderers can apply them
// directly against raw.
func computeWordHighlights(lines []diffLine) {
	i := 0
	for i < len(lines) {
		if lines[i].side != "old" {
			i++
			continue
		}
		dStart := i
		for i < len(lines) && lines[i].side == "old" {
			i++
		}
		aStart := i
		for i < len(lines) && lines[i].side == "new" {
			i++
		}
		dels := lines[dStart:aStart]
		adds := lines[aStart:i]
		n := len(dels)
		if len(adds) < n {
			n = len(adds)
		}
		for k := 0; k < n; k++ {
			oldBody := strings.TrimPrefix(dels[k].raw, "-")
			newBody := strings.TrimPrefix(adds[k].raw, "+")
			oR, nR := wordDiffRanges(oldBody, newBody)
			// Shift back to include the leading "-"/"+" character.
			for r := range oR {
				oR[r].start++
				oR[r].end++
			}
			for r := range nR {
				nR[r].start++
				nR[r].end++
			}
			dels[k].highlights = oR
			adds[k].highlights = nR
		}
	}
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

	// Word-diff overlay: highlights are byte ranges within raw and
	// hlStyle is the style to paint over them. Empty highlights → no
	// overlay (the cell renders as a single styled run).
	highlights []hlRange
	hlStyle    lipgloss.Style
}

func (c diffCell) commentable() bool { return c.path != "" && c.side != "" && c.line > 0 }

// diffRow is one logical row in the rendered diff. In unified mode
// only cells[0] is populated; in split mode cells[0] is the old (LHS)
// side and cells[1] is the new (RHS) side. Hunk and file headers use
// fullWidth=true to span both columns.
type diffRow struct {
	cells        [2]diffCell
	fullWidth    bool
	annotation   bool           // comment annotation row (no anchor of its own)
	annoText     string         // plain text (no ANSI) for annotation
	annoStyle    lipgloss.Style // style applied at render time at the right width
	annoSide     int            // 0 = old column, 1 = new column (split only)
	annoIsHeader bool           // header (author/timestamp) vs body line of a comment
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
		m.diffRows = injectInlineComments(m.diffRows, m.diffComments, m.annotationWidth())
	}
	m.diffFiles = computeDiffFiles(m.diffRows)
	m.diffTree = computeFileTree(m.diffFiles)
	m.diff.SetContent(m.renderDiffRows())
	m.clampDiffCursor()
	m.syncTreeCursor()
}

// annotationWidth returns the wrap width (in cells) for inline-comment
// annotation text in the current layout. Reserves space for the row
// marker, gutter glyphs and indent so wrapped lines line up with the
// code they're attached to.
func (m model) annotationWidth() int {
	width := m.diff.Width
	if width <= 0 {
		width = 80
	}
	if m.diffSplit {
		// Same column maths as renderDiffRows, minus the gutter
		// glyphs we prepend ("│  " = 3 cells, plus 1 cell of slack).
		colW := (width - 5) / 2
		w := colW - 4
		if w < 12 {
			w = 12
		}
		return w
	}
	// Unified: 2 cells marker + 4 cells indent + 2 cells gutter glyphs.
	w := width - 8
	if w < 20 {
		w = 20
	}
	return w
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

// hlStyleFor maps a diff side to its word-diff overlay style.
func hlStyleFor(side string) lipgloss.Style {
	switch side {
	case "new":
		return diffAddHL
	case "old":
		return diffDelHL
	}
	return lipgloss.NewStyle()
}

func buildUnifiedRows(lines []diffLine) []diffRow {
	rows := make([]diffRow, 0, len(lines))
	for _, dl := range lines {
		c := diffCell{raw: dl.raw, style: dl.style}
		if dl.commentable() {
			side, lineNo := inlineSide(dl)
			c.path, c.side, c.line = dl.path, side, lineNo
		}
		if len(dl.highlights) > 0 {
			c.highlights = dl.highlights
			c.hlStyle = hlStyleFor(dl.side)
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
					row.cells[0] = diffCell{
						raw: d.raw, style: d.style,
						path: d.path, side: "old", line: d.lineNo,
						highlights: d.highlights, hlStyle: diffDelHL,
					}
				}
				if j < len(adds) {
					a := adds[j]
					row.cells[1] = diffCell{
						raw: a.raw, style: a.style,
						path: a.path, side: "new", line: a.lineNo,
						highlights: a.highlights, hlStyle: diffAddHL,
					}
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

// injectInlineComments walks the row list and inserts annotation rows
// immediately after each row that has a matching inline comment
// anchor. Comments are word-wrapped to wrapW so the full body is
// visible (rather than truncated to one line); each emitted row is
// one wrapped line so cursor / viewport maths stay row-accurate.
func injectInlineComments(rows []diffRow, comments []api.Comment, wrapW int) []diffRow {
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
			for _, c := range byAnchor[key{cell.path, cell.side, cell.line}] {
				for _, line := range formatInlineAnnotationLines(c, wrapW) {
					out = append(out, diffRow{
						annotation:   true,
						annoText:     line.text,
						annoStyle:    line.style,
						annoSide:     idx,
						annoIsHeader: line.header,
					})
				}
			}
		}
	}
	return out
}

type annoTextLine struct {
	text   string
	style  lipgloss.Style
	header bool
}

// formatInlineAnnotationLines splits a comment into a header line
// (author + relative timestamp) followed by word-wrapped body lines.
// Each returned line carries its own style so the renderer can draw
// the header bold and the body soft/italic.
func formatInlineAnnotationLines(c api.Comment, wrapW int) []annoTextLine {
	if wrapW < 8 {
		wrapW = 8
	}
	body := strings.TrimSpace(c.Text)
	if body == "" {
		body = "(empty)"
	}
	header := "💬 " + c.Author
	if !c.CreatedAt.IsZero() {
		header += "  " + humanTime(c.CreatedAt)
	}

	out := []annoTextLine{}
	for _, w := range wrapText(header, wrapW) {
		out = append(out, annoTextLine{text: w, style: annoHeaderStyle, header: true})
	}
	for _, raw := range strings.Split(body, "\n") {
		wrapped := wrapText(raw, wrapW)
		if len(wrapped) == 0 {
			wrapped = []string{""}
		}
		for _, w := range wrapped {
			out = append(out, annoTextLine{text: w, style: annoBodyStyle})
		}
	}
	return out
}

// wrapText word-wraps s to fit width w (in terminal cells), breaking
// on whitespace where possible and falling back to hard rune-level
// breaks for tokens longer than w. Empty input yields one empty line.
func wrapText(s string, w int) []string {
	if w <= 0 {
		return []string{s}
	}
	if lipgloss.Width(s) <= w {
		return []string{s}
	}
	var out []string
	var cur strings.Builder
	curW := 0
	flush := func() {
		out = append(out, cur.String())
		cur.Reset()
		curW = 0
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	for _, word := range words {
		ww := lipgloss.Width(word)
		// Break long words across multiple lines.
		if ww > w {
			if curW > 0 {
				flush()
			}
			runes := []rune(word)
			for len(runes) > 0 {
				take := 0
				wAcc := 0
				for take < len(runes) {
					rw := lipgloss.Width(string(runes[take]))
					if wAcc+rw > w {
						break
					}
					wAcc += rw
					take++
				}
				if take == 0 {
					take = 1
				}
				out = append(out, string(runes[:take]))
				runes = runes[take:]
			}
			continue
		}
		extra := ww
		if curW > 0 {
			extra++ // leading space
		}
		if curW+extra > w {
			flush()
			cur.WriteString(word)
			curW = ww
		} else {
			if curW > 0 {
				cur.WriteByte(' ')
				curW++
			}
			cur.WriteString(word)
			curW += ww
		}
	}
	if curW > 0 {
		flush()
	}
	return out
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
		// gutterFor returns the styled left bar for an annotation row
		// — bright slab on header lines, thin bar on body lines, both
		// painted with the same background as the surrounding text so
		// the comment block reads as one continuous chip.
		gutterFor := func(headerLine bool) (string, lipgloss.Style) {
			if headerLine {
				return annoGutterStyle.Render("▎ "), annoGutterStyle
			}
			return annoBodyGutter.Render("│ "), annoBodyGutter
		}
		switch {
		case row.annotation:
			b.WriteString(leftPad)
			glyph, gStyle := gutterFor(row.annoIsHeader)
			if m.diffSplit {
				inner := colW - lipgloss.Width(glyph)
				if inner < 1 {
					inner = 1
				}
				text := truncateForCell(row.annoText, inner)
				styled := row.annoStyle.Width(inner).MaxWidth(inner).Render(text)
				cell := glyph + styled
				blank := strings.Repeat(" ", colW)
				if row.annoSide == 0 {
					b.WriteString(cell)
					b.WriteString(gStyle.Render(" │ "))
					b.WriteString(blank)
				} else {
					b.WriteString(blank)
					b.WriteString(gStyle.Render(" │ "))
					b.WriteString(cell)
				}
			} else {
				// Pad the body so the comment background spans the
				// remaining viewport width, mimicking Bitbucket's
				// commented-line block.
				body := width - 2 - 4 - lipgloss.Width(glyph)
				if body < 1 {
					body = 1
				}
				text := truncateForCell(row.annoText, body)
				b.WriteString(gStyle.Render("    "))
				b.WriteString(glyph)
				b.WriteString(row.annoStyle.Width(body).MaxWidth(body).Render(text))
			}
		case row.fullWidth:
			b.WriteString(marker)
			body := width - 2
			if body < 1 {
				body = 1
			}
			b.WriteString(row.cells[0].style.Width(body).MaxWidth(body).Render(row.cells[0].raw))
		case m.diffSplit:
			b.WriteString(marker)
			b.WriteString(renderSplitCell(row.cells[0], colW, i == m.diffCursor && m.diffCursorSide == 0))
			b.WriteString(" │ ")
			b.WriteString(renderSplitCell(row.cells[1], colW, i == m.diffCursor && m.diffCursorSide == 1))
		default:
			b.WriteString(marker)
			body := width - 2
			if body < 1 {
				body = 1
			}
			b.WriteString(renderCellWithHL(row.cells[0], body))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// renderSplitCell renders one cell of a split row, padding/truncating
// to width. The active-side flag draws an underline so the user can
// see which column 'c' will target.
func renderSplitCell(c diffCell, w int, active bool) string {
	if c.raw == "" {
		return strings.Repeat(" ", w)
	}
	if active {
		c.style = c.style.Underline(true)
		if len(c.highlights) > 0 {
			c.hlStyle = c.hlStyle.Underline(true)
		}
	}
	return renderCellWithHL(c, w)
}

// renderCellWithHL renders a diff cell honouring its word-diff
// highlight ranges. Falls back to the cheap "single styled run with
// width padding" when there are no highlights or when the line is
// wider than the column (in which case lipgloss handles truncation
// for us).
func renderCellWithHL(c diffCell, w int) string {
	if len(c.highlights) == 0 || lipgloss.Width(c.raw) > w {
		return c.style.Width(w).MaxWidth(w).Render(c.raw)
	}
	var b strings.Builder
	cur := 0
	for _, h := range c.highlights {
		if h.start < cur {
			h.start = cur
		}
		if h.start > len(c.raw) {
			break
		}
		if h.end > len(c.raw) {
			h.end = len(c.raw)
		}
		if h.start > cur {
			b.WriteString(c.style.Render(c.raw[cur:h.start]))
		}
		if h.end > h.start {
			b.WriteString(c.hlStyle.Render(c.raw[h.start:h.end]))
		}
		cur = h.end
	}
	if cur < len(c.raw) {
		b.WriteString(c.style.Render(c.raw[cur:]))
	}
	have := lipgloss.Width(b.String())
	if w > have {
		b.WriteString(c.style.Render(strings.Repeat(" ", w-have)))
	}
	return b.String()
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
