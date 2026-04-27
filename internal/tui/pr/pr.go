// Package tui contains Bubble Tea models for bb's interactive mode.
//
// PRs() returns a runnable program that lets the user browse and act on
// pull requests for a given (host, project, slug).
package pr

import (
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hugs7/bitbucket-cli/internal/aiutil"
	"github.com/hugs7/bitbucket-cli/internal/api"
	"github.com/hugs7/bitbucket-cli/internal/tui/theme"
	"github.com/hugs7/bitbucket-cli/internal/config"
	"github.com/hugs7/bitbucket-cli/internal/strutil"
	"github.com/hugs7/bitbucket-cli/internal/sysutil"
)

// PRs launches the interactive pull-requests TUI.
func Run(svc api.Service, project, slug string) error {
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
	viewConfirmDecline
	viewConfirmMerge
	viewPalette
	viewEditor
	viewSettings
)

// buildDot is a thin alias for theme.BuildDot so the dozens of
// in-file callers stay terse.
func buildDot(state string) string { return theme.BuildDot(state) }


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
	// pendingDeleteFromDiff indicates the delete was triggered from
	// viewDiff (vs viewComments) so the post-delete reload should refresh
	// the diff overlay and stay in the diff view.
	pendingDeleteFromDiff bool

	// when set, we're in a decline-PR confirm sub-mode
	pendingDeclinePRID int

	// merge-confirm sub-mode state. pendingMergeDeleteBranch toggles
	// whether to remove the source branch after a successful merge;
	// the user flips it with 'd' on the confirm screen.
	pendingMergePRID         int
	pendingMergeSourceRef    string
	pendingMergeDeleteBranch bool

	// prBuilds caches the latest build state per PR id so the list
	// can render a status dot without re-fetching on every keystroke.
	prBuilds map[int]string

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
	// know where to drop back to. The widget renders as a centred
	// overlay card on top of the underlying view.
	palette         paletteWidget
	paletteReturnTo viewMode

	// inline / PIP editor state. editor holds the textarea bubble
	// while the user is composing; editorReturnTo remembers the mode
	// we came from so save / cancel can restore it.
	editor         inlineEditor
	editorActive   bool
	editorReturnTo viewMode

	// pty is the running embedded editor (vim/neovim/etc) when an
	// inline-diff comment is being composed. Nil when no PTY editor
	// is active. While active, keystrokes flow into the PTY and the
	// diff renderer injects the editor's screen between code lines.
	pty *ptyEditor

	// settings overlay: a list of toggle/cycle items backed by the
	// persisted config. settingsReturnTo is the mode we came from so
	// esc drops back to where the user opened it.
	settings         list.Model
	settingsReturnTo viewMode
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
	theme.Init()
	delegate := list.NewDefaultDelegate()
	l := list.New(nil, delegate, 0, 0)
	l.Title = fmt.Sprintf("Pull Requests · %s/%s", project, slug)
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.Styles.Title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))

	sp := spinner.New()
	// |/-\ under 3270 to match old TTY cadence; braille dot otherwise.
	if theme.Mainframe() {
		sp.Spinner = spinner.Line
	} else {
		sp.Spinner = spinner.Dot
	}
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))

	cdel := list.NewDefaultDelegate()
	cl := list.New(nil, cdel, 0, 0)
	cl.SetShowTitle(false)
	cl.SetShowStatusBar(false)
	cl.SetFilteringEnabled(true)

	sdel := list.NewDefaultDelegate()
	sl := list.New(nil, sdel, 0, 0)
	sl.Title = "Settings"
	sl.SetShowStatusBar(false)
	sl.SetFilteringEnabled(false)
	sl.SetShowHelp(false)
	sl.Styles.Title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))

	cfg := config.Get()
	return model{
		svc: svc, project: project, slug: slug,
		state:          "OPEN",
		list:           l,
		detail:         viewport.New(0, 0),
		diff:           viewport.New(0, 0),
		comments:       cl,
		palette:        newPaletteWidget(),
		settings:       sl,
		spinner:        sp,
		help:           help.New(),
		keys:           defaultKeys(),
		loading:        true,
		diffSplit:      cfg.DiffSplit,
		diffShowInline: !cfg.DiffHideInline,
	}
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

// fetchBuildForPR queries the latest build for a PR's source ref and
// returns the resulting state via prBuildLoadedMsg. Failures are
// silently swallowed (the PR list still works without a status dot).
func (m *model) fetchBuildForPR(pr api.PullRequest) tea.Cmd {
	prID := pr.ID
	ref := pr.SourceRef
	return func() tea.Msg {
		if ref == "" {
			return prBuildLoadedMsg{prID: prID}
		}
		builds, err := m.svc.ListBuildsForRef(m.project, m.slug, ref, 5)
		if err != nil || len(builds) == 0 {
			return prBuildLoadedMsg{prID: prID}
		}
		return prBuildLoadedMsg{prID: prID, state: builds[0].State}
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

// currentFilePath returns the path of the file the diff cursor is
// currently inside, by walking the file-header index. Empty when
// no diff is loaded.
func (m *model) currentFilePath() string {
	activePath := ""
	for _, f := range m.diffFiles {
		if f.rowIdx <= m.diffCursor {
			activePath = f.path
		}
	}
	return activePath
}

// diffCommentMutation runs a comment-changing API call, then re-fetches
// comments and posts diffCommentsLoadedMsg so the diff overlay updates
// without leaving the diff view.
func (m *model) diffCommentMutation(prID int, label string, fn func() error) tea.Cmd {
	m.loading = true
	return tea.Batch(m.spinner.Tick, func() tea.Msg {
		if err := fn(); err != nil {
			return actionDoneMsg{text: label, err: err}
		}
		cs, err := m.svc.ListComments(m.project, m.slug, prID)
		if err != nil {
			return actionDoneMsg{text: label + " (overlay reload failed)", err: err}
		}
		return diffCommentsLoadedMsg{id: prID, comments: cs}
	})
}

// applyDiffJump moves the diff cursor to the first commentable row
// matching the given target anchor. Returns true if a row matched.
// A relaxed match (path+line, ignoring side) is tried as a fallback
// because context lines in unified mode default to "new" but the user
// may have anchored a comment to the "old" side.
func (m *model) applyDiffJump(t diffJumpTarget) bool {
	for i, r := range m.diffRows {
		if r.fullWidth || r.annotation {
			continue
		}
		for _, c := range r.cells {
			if c.commentable() && c.path == t.path && c.side == t.side && c.line == t.line {
				m.diffCursor = i
				return true
			}
		}
	}
	for i, r := range m.diffRows {
		if r.fullWidth || r.annotation {
			continue
		}
		for _, c := range r.cells {
			if c.commentable() && c.path == t.path && c.line == t.line {
				m.diffCursor = i
				return true
			}
		}
	}
	return false
}

func (m *model) doAction(label string, reload bool, fn func() error) tea.Cmd {
	return func() tea.Msg {
		err := fn()
		return actionDoneMsg{text: label, err: err, reload: reload}
	}
}

// inline (PIP) overlay and the historical $EDITOR fullscreen flow
// based on config.InlineEditor. Call sites stay unchanged.
func editInTUI(purpose, hint string, prID, commentID int, initial string) tea.Cmd {
	return requestEditPR(purpose, hint, prID, commentID, initial)
}

// fetchAIDescription pulls the diff for the given PR, pipes it
// through the configured AI command, and returns the result as an
// aiDescribeDoneMsg. The TUI then opens the description editor
// pre-seeded with the suggestion so the user can edit before saving.
func (m *model) fetchAIDescription(prID int) tea.Cmd {
	project, slug := m.project, m.slug
	return func() tea.Msg {
		aiCmd := aiutil.Resolve("")
		if aiCmd == "" {
			return aiDescribeDoneMsg{prID: prID, err: fmt.Errorf("no AI command configured (ai_cmd / $BB_AI_CMD)")}
		}
		diff, err := m.svc.PRDiff(project, slug, prID)
		if err != nil {
			return aiDescribeDoneMsg{prID: prID, err: err}
		}
		if strings.TrimSpace(diff) == "" {
			return aiDescribeDoneMsg{prID: prID, err: fmt.Errorf("PR has an empty diff")}
		}
		out, err := aiutil.Run(aiCmd, diff)
		return aiDescribeDoneMsg{prID: prID, text: strings.TrimSpace(out), err: err}
	}
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
		// Forward the resize to the embedded PTY editor too — nvim
		// caches its window size from the pty(7) ioctl and won't
		// reflow on its own.
		if m.pty != nil && m.pty.Active() {
			m.pty.Resize(ptyInlineCols(m.diff.Width), ptyInlineRows(m.diff.Height))
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
			cmd := m.palette.Update(msg)
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

	case theme.ChangedMsg:
		// theme.Apply has already swapped the package-level styles;
		// just persist the choice and surface a toast so the demo
		// gets a visible confirmation each cycle.
		_ = config.SetTheme(msg.Name)
		m.status = "✓ theme: " + msg.Name
		return m, nil

	case prsLoadedMsg:
		m.loading = false
		// Reset the build cache so a stale dot from a previous tab
		// (e.g. switching OPEN → MERGED) doesn't linger on a PR that
		// no longer matches.
		m.prBuilds = make(map[int]string, len(msg.prs))
		// Compute stacks once per load so each list item can render
		// its position cheaply via map lookup.
		stackPos := computeStackPositions(msg.prs)
		items := make([]list.Item, 0, len(msg.prs))
		var buildCmds []tea.Cmd
		for _, p := range msg.prs {
			it := prItem{pr: p}
			if sp, ok := stackPos[p.ID]; ok {
				it.stackPos, it.stackTotal = sp.pos, sp.total
			}
			items = append(items, it)
			buildCmds = append(buildCmds, m.fetchBuildForPR(p))
		}
		m.list.SetItems(items)
		m.updateDetail()
		if len(buildCmds) == 0 {
			return m, nil
		}
		return m, tea.Batch(buildCmds...)

	case prBuildLoadedMsg:
		if m.prBuilds == nil {
			m.prBuilds = map[int]string{}
		}
		m.prBuilds[msg.prID] = msg.state
		// Patch the matching list item so the dot appears without a
		// full SetItems rebuild (preserves cursor/filter state).
		items := m.list.Items()
		for i, it := range items {
			if pi, ok := it.(prItem); ok && pi.pr.ID == msg.prID {
				pi.buildState = msg.state
				m.list.SetItem(i, pi)
				break
			}
		}
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
		// place the cursor on the matching row. The pending jump is
		// preserved so it can be re-applied once comments have loaded
		// — annotation rows shift indices by their count, which would
		// otherwise leave the cursor a few rows above the target.
		jumped := false
		if m.diffPendingJump != nil {
			jumped = m.applyDiffJump(*m.diffPendingJump)
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
		// Comments already cached — clear the pending jump so a later
		// stray load doesn't snap the cursor again.
		m.diffPendingJump = nil
		return m, nil

	case diffCommentsLoadedMsg:
		m.loading = false
		m.diffCommentsPRID = msg.id
		m.diffComments = msg.comments
		if m.mode == viewDiff && m.diffID == msg.id {
			m.rebuildDiffRows()
			// Re-apply the pending jump after annotation rows have
			// been injected — they shift indices so the cursor placed
			// in diffLoadedMsg now points one or more rows above the
			// real anchor.
			if m.diffPendingJump != nil {
				m.applyDiffJump(*m.diffPendingJump)
				m.diffPendingJump = nil
				m.syncTreeCursor()
				m.diff.SetContent(m.renderDiffRows())
			}
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

	case ptyTickMsg:
		// Periodic refresh while the embedded editor is active so
		// vt10x's screen state shows up in the diff. Re-arm only
		// while still active to avoid a busy-loop after exit.
		if m.pty != nil && m.pty.Active() {
			m.diff.SetContent(m.renderDiffRows())
			return m, ptyTick()
		}
		return m, nil

	case ptyExitedMsg:
		// Editor process finished. Read the temp file (= comment
		// text) and dispatch the same editorResultMsg the textarea
		// path emits so the existing save plumbing handles it.
		if m.pty == nil {
			return m, nil
		}
		text, rerr := m.pty.ReadResult()
		req := m.pty.req
		m.pty.Close()
		m.pty = nil
		// Reclaim the screen rows we reserved for the editor pane in
		// layout() so the diff viewport stretches back to full height.
		m.layout()
		m.diff.SetContent(m.renderDiffRows())
		if msg.err != nil {
			rerr = msg.err
		}
		return m, func() tea.Msg { return editorResultFor(req, text, rerr) }

	case aiDescribeDoneMsg:
		m.loading = false
		if msg.err != nil {
			m.status = "✗ ai describe: " + msg.err.Error()
			return m, nil
		}
		m.status = fmt.Sprintf("✓ AI description ready · review & save (#%d)", msg.prID)
		// Open the description editor with the AI suggestion as the
		// initial buffer; existing edit-description flow handles save.
		return m, editInTUI("edit-description",
			fmt.Sprintf("pr-%d-ai-description", msg.prID), msg.prID, 0, msg.text)

	case wantEditMsg:
		// Branch on the user's preference: inline (PIP) overlay or
		// classic full-screen $EDITOR. The inline path stays in
		// process; the fullscreen path returns a tea.ExecProcess Cmd
		// that suspends until the editor exits.
		// Diff-originated edits spawn the user's real editor (vim,
		// neovim, helix, …) inside a PTY rendered inline between
		// the diff lines. Saving the file in the editor commits the
		// comment; quitting without saving an empty file aborts.
		if m.mode == viewDiff && isDiffOriginated(msg.req.purpose) {
			// PTY-embedded editor (vim/nvim inside a pseudo-terminal
			// rendered as a fixed pane below the diff viewport) is
			// opt-in via the "pty_editor" setting. Default off so the
			// fullscreen $EDITOR fallback handles every platform out
			// of the box. Windows ConPTY still has known vt10x
			// rendering issues, so we keep that platform pinned to
			// fullscreen even when the toggle is on.
			if !config.Get().PTYEditor || runtime.GOOS == "windows" {
				return m, runFullscreenEditor(msg.req)
			}
			rows := ptyInlineRows(m.diff.Height)
			cols := ptyInlineCols(m.diff.Width)
			pe, startCmd, err := newPTYEditor(msg.req, cols, rows)
			if err != nil {
				m.status = "✗ editor: " + err.Error()
				return m, nil
			}
			m.pty = pe
			m.editorReturnTo = m.mode
			// Re-layout NOW so the diff viewport shrinks to make room
			// for the fixed-pane PTY editor below it. Without this
			// the editor pane spills past the bottom of the terminal
			// and looks "wedged".
			m.layout()
			m.diff.SetContent(m.renderDiffRows())
			m.ensureDiffCursorVisible()
			return m, startCmd
		}
		// Non-diff edits keep the centred textarea overlay —
		// description / PR-level comments still get their roomy
		// modal so users have room to write long-form text.
		if config.Get().InlineEditor {
			m.editor = newInlineEditor(msg.req, m.width, m.height)
			m.editorActive = true
			m.editorReturnTo = m.mode
			m.mode = viewEditor
			return m, textarea.Blink
		}
		return m, runFullscreenEditor(msg.req)

	case editorResultMsg:
		text := strings.TrimSpace(msg.text)
		if msg.err != nil {
			m.status = "✗ editor: " + msg.err.Error()
			return m, nil
		}
		// "create-pr" needs to peek at structured fields before the
		// generic "empty buffer" check, since the template itself is
		// non-empty even when the user hasn't filled anything in.
		if msg.purpose == "create-pr" {
			title, source, target, body := parseCreatePRTemplate(msg.text)
			if title == "" {
				m.status = "aborted (no title)"
				return m, nil
			}
			if source == "" || target == "" {
				m.status = "✗ source and target branches are required"
				return m, nil
			}
			in := api.CreatePRInput{
				Title:       title,
				Description: body,
				SourceRef:   source,
				TargetRef:   target,
			}
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, func() tea.Msg {
				p, err := m.svc.CreatePR(m.project, m.slug, in)
				if err != nil {
					return actionDoneMsg{text: "create PR", err: err}
				}
				return actionDoneMsg{text: fmt.Sprintf("created PR #%d", p.ID), reload: true}
			})
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
		case "edit-comment-diff":
			cID := msg.commentID
			prID := msg.prID
			return m, m.diffCommentMutation(prID, fmt.Sprintf("edited #%d", cID), func() error {
				_, err := m.svc.EditComment(m.project, m.slug, prID, cID, text)
				return err
			})
		case "add-comment-diff":
			prID := msg.prID
			return m, m.diffCommentMutation(prID, "added PR comment", func() error {
				_, err := m.svc.AddComment(m.project, m.slug, prID, text)
				return err
			})
		case "add-reviewer":
			prID := msg.prID
			users := splitFirstLineTokens(text)
			if len(users) == 0 {
				m.status = "no reviewer username provided"
				return m, nil
			}
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, m.doAction(fmt.Sprintf("added reviewer(s) %s", strings.Join(users, ", ")), true, func() error {
				return m.svc.AddReviewers(m.project, m.slug, prID, users)
			}))
		case "remove-reviewer":
			prID := msg.prID
			users := splitFirstLineTokens(text)
			if len(users) == 0 {
				m.status = "no reviewer username provided"
				return m, nil
			}
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, m.doAction(fmt.Sprintf("removed reviewer(s) %s", strings.Join(users, ", ")), true, func() error {
				return m.svc.RemoveReviewers(m.project, m.slug, prID, users)
			}))
		case "add-inline-comment":
			path := msg.path
			line := msg.line
			side := msg.side
			prID := msg.prID
			label := fmt.Sprintf("inline comment on %s:%d (%s)", path, line, side)
			if line == 0 {
				label = fmt.Sprintf("file comment on %s", path)
			}
			return m, m.diffCommentMutation(prID, label, func() error {
				_, err := m.svc.AddInlineComment(m.project, m.slug, prID, api.InlineCommentInput{
					Text: text, Path: path, Line: line, Side: side,
				})
				return err
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

		// Palette mode owns the keymap entirely while open. Esc
		// closes; arrows navigate; enter runs the highlighted item.
		// Every other key flows into the textinput so typing
		// immediately filters (no need to press '/' first).
		if m.mode == viewPalette {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "esc":
				m.mode = m.paletteReturnTo
				return m, nil
			case "enter":
				if it, ok := m.palette.SelectedItem(); ok {
					m.mode = m.paletteReturnTo
					return m, it.run(&m)
				}
				return m, nil
			case "up", "ctrl+p":
				m.palette.MoveCursor(-1)
				return m, nil
			case "down", "ctrl+n":
				m.palette.MoveCursor(+1)
				return m, nil
			case "pgup":
				m.palette.MoveCursor(-10)
				return m, nil
			case "pgdown":
				m.palette.MoveCursor(+10)
				return m, nil
			}
			cmd := m.palette.Update(msg)
			return m, cmd
		}

		// PTY-embedded editor owns *every* keystroke while active so
		// vim-style commands (esc, :w, /search, …) reach the editor
		// instead of being interpreted as bb shortcuts. The exception
		// is ctrl+c (and ctrl+\): users expect these to escape, and a
		// hung / wedged editor leaves the TUI completely unresponsive
		// otherwise. Both kill the editor process and emit
		// ptyExitedMsg so the standard cleanup path runs.
		if m.pty != nil && m.pty.Active() {
			switch msg.String() {
			case "ctrl+c", "ctrl+\\":
				pe := m.pty
				pe.Close()
				return m, func() tea.Msg {
					return ptyExitedMsg{err: fmt.Errorf("aborted")}
				}
			}
			m.pty.SendKey(msg)
			m.diff.SetContent(m.renderDiffRows())
			return m, nil
		}

		// Inline (PIP) editor owns the keymap while active. ctrl+s
		// commits the buffer; esc cancels; F11 promotes the current
		// content into the user's $EDITOR (so a long edit can switch
		// to vim mid-thought without losing the draft).
		if m.editorActive && m.mode == viewEditor {
			return m.handleEditorKey(msg)
		}

		// Settings overlay owns the keymap while open.
		if m.mode == viewSettings {
			switch {
			case key.Matches(msg, m.keys.Quit):
				return m, tea.Quit
			case key.Matches(msg, m.keys.Back):
				m.mode = m.settingsReturnTo
				return m, nil
			case key.Matches(msg, m.keys.ClearStatus):
				m.status = ""
				m.err = nil
				return m, nil
			case key.Matches(msg, m.keys.SettingsToggle):
				if it, ok := m.settings.SelectedItem().(settingItem); ok {
					cmd := it.toggleFn(&m)
					m.refreshSettings()
					return m, cmd
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.settings, cmd = m.settings.Update(msg)
			return m, cmd
		}

		// Mode-independent keys come first.
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.ClearStatus):
			// Clear any transient toast / error so it stops shadowing
			// the help bar at the bottom of the screen.
			m.status = ""
			m.err = nil
			return m, nil
		case key.Matches(msg, m.keys.Settings):
			// Don't intercept "," when the user is mid-count in the
			// diff view (numBuf != "") so digit motions stay clean.
			if m.mode == viewDiff && m.numBuf != "" {
				break
			}
			m.openSettings()
			return m, nil
		case key.Matches(msg, m.keys.PaletteOpen):
			// ":" colon also opens the palette but we must not let it
			// trigger when the user is filtering a list (see top-of
			// block check). Skip in viewDiff if a count is being typed
			// (rare; keep simple by checking numBuf).
			return m, m.openPalette()
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
			case key.Matches(msg, m.keys.DiffFileComment):
				path := m.currentFilePath()
				if path == "" {
					m.status = "no file under cursor"
					return m, nil
				}
				return m, editInlineInTUI(m.diffID, path, 0, "new")
			case key.Matches(msg, m.keys.DiffAddComment):
				if m.diffID > 0 {
					return m, editInTUI("add-comment-diff",
						fmt.Sprintf("pr-%d-comment", m.diffID), m.diffID, 0, "")
				}
				return m, nil
			case key.Matches(msg, m.keys.DiffEditComment):
				cm, ok := m.commentAtCursor()
				if !ok {
					m.status = "no comment at cursor — move to a commented line"
					return m, nil
				}
				return m, editInTUI("edit-comment-diff",
					fmt.Sprintf("pr-%d-comment-%d", m.diffID, cm.ID),
					m.diffID, cm.ID, cm.Text)
			case key.Matches(msg, m.keys.DiffDeleteComment):
				cm, ok := m.commentAtCursor()
				if !ok {
					m.status = "no comment at cursor — move to a commented line"
					return m, nil
				}
				m.pendingDeleteCommentID = cm.ID
				m.commentsPRID = m.diffID
				m.pendingDeleteFromDiff = true
				m.mode = viewConfirmDelete
				return m, nil
			case key.Matches(msg, m.keys.DiffReactComment):
				cm, ok := m.commentAtCursor()
				if !ok {
					m.status = "no comment at cursor — move to a commented line"
					return m, nil
				}
				prID := m.diffID
				cID := cm.ID
				return m, m.diffCommentMutation(prID, fmt.Sprintf("reacted 👍 to #%d", cID), func() error {
					return m.svc.AddReaction(m.project, m.slug, prID, cID, "thumbsup")
				})
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
					m.pendingMergePRID = id
					m.pendingMergeSourceRef = it.pr.SourceRef
					m.pendingMergeDeleteBranch = false
					m.mode = viewConfirmMerge
					return m, nil
				case key.Matches(msg, m.keys.EditDesc):
					return m, editInTUI("edit-description",
						fmt.Sprintf("pr-%d-description", id), id, 0, it.pr.Description)
				case key.Matches(msg, m.keys.CreatePR):
					return m, m.startCreatePR()
				case key.Matches(msg, m.keys.DeclinePR):
					m.pendingDeclinePRID = id
					m.mode = viewConfirmDecline
					return m, nil
				case key.Matches(msg, m.keys.AddReviewer):
					return m, editInTUI("add-reviewer",
						fmt.Sprintf("pr-%d-add-reviewer", id), id, 0,
						"# Enter one or more usernames (Server) or UUIDs/emails\n"+
							"# (Cloud), separated by space or comma. First non-comment\n"+
							"# line is used. Save & exit to submit; empty cancels.\n")
				case key.Matches(msg, m.keys.RemoveReviewer):
					return m, editInTUI("remove-reviewer",
						fmt.Sprintf("pr-%d-remove-reviewer", id), id, 0,
						reviewerListHint(it.pr.Reviewers))
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
				fromDiff := m.pendingDeleteFromDiff
				m.pendingDeleteCommentID = 0
				m.pendingDeleteFromDiff = false
				if fromDiff {
					m.mode = viewDiff
					return m, m.diffCommentMutation(prID, fmt.Sprintf("deleted #%d", cID), func() error {
						return m.svc.DeleteComment(m.project, m.slug, prID, cID)
					})
				}
				m.mode = viewComments
				return m, m.commentMutation(prID, fmt.Sprintf("deleted #%d", cID), func() error {
					return m.svc.DeleteComment(m.project, m.slug, prID, cID)
				})
			case key.Matches(msg, m.keys.ConfirmNo):
				m.pendingDeleteCommentID = 0
				if m.pendingDeleteFromDiff {
					m.mode = viewDiff
					m.pendingDeleteFromDiff = false
				} else {
					m.mode = viewComments
				}
				m.status = "delete cancelled"
				return m, nil
			}
			return m, nil
		case viewConfirmDecline:
			switch {
			case key.Matches(msg, m.keys.ConfirmYes):
				prID := m.pendingDeclinePRID
				m.pendingDeclinePRID = 0
				m.mode = viewList
				m.loading = true
				return m, tea.Batch(m.spinner.Tick, m.doAction(fmt.Sprintf("declined #%d", prID), true, func() error {
					return m.svc.DeclinePR(m.project, m.slug, prID)
				}))
			case key.Matches(msg, m.keys.ConfirmNo):
				m.pendingDeclinePRID = 0
				m.mode = viewList
				m.status = "decline cancelled"
				return m, nil
			}
			return m, nil
		case viewConfirmMerge:
			switch msg.String() {
			case "d":
				// Toggle "delete source branch on merge" without
				// leaving the confirm screen so the user can flip
				// their mind before pressing y.
				m.pendingMergeDeleteBranch = !m.pendingMergeDeleteBranch
				return m, nil
			case "y", "Y", "enter":
				prID := m.pendingMergePRID
				src := m.pendingMergeSourceRef
				del := m.pendingMergeDeleteBranch
				m.pendingMergePRID = 0
				m.pendingMergeSourceRef = ""
				m.pendingMergeDeleteBranch = false
				m.mode = viewList
				m.loading = true
				label := fmt.Sprintf("merged #%d", prID)
				if del {
					label = fmt.Sprintf("merged #%d + deleted %s", prID, src)
				}
				return m, tea.Batch(m.spinner.Tick, m.doAction(label, true, func() error {
					if err := m.svc.MergePR(m.project, m.slug, prID); err != nil {
						return err
					}
					if del && src != "" {
						// Branch deletion is best-effort: the merge
						// already succeeded, so surface the failure
						// in the toast but don't undo the merge.
						if derr := m.svc.DeleteBranch(m.project, m.slug, src); derr != nil {
							return fmt.Errorf("merged but branch delete failed: %w", derr)
						}
					}
					return nil
				}))
			case "n", "N", "esc":
				m.pendingMergePRID = 0
				m.pendingMergeSourceRef = ""
				m.pendingMergeDeleteBranch = false
				m.mode = viewList
				m.status = "merge cancelled"
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
		case key.Matches(msg, m.keys.NextFile):
			// In the list view, ]/[ jump within the current stack
			// (next / prev PR by source-target chain). Falls through
			// to the list (default Update) when the selected PR is
			// standalone so users still get any list-default keybind.
			if m.jumpStack(+1) {
				return m, nil
			}
		case key.Matches(msg, m.keys.PrevFile):
			if m.jumpStack(-1) {
				return m, nil
			}
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
				m.pendingMergePRID = it.pr.ID
				m.pendingMergeSourceRef = it.pr.SourceRef
				m.pendingMergeDeleteBranch = false
				m.mode = viewConfirmMerge
				return m, nil
			}
		case key.Matches(msg, m.keys.EditDesc):
			if it, ok := m.list.SelectedItem().(prItem); ok {
				return m, editInTUI("edit-description",
					fmt.Sprintf("pr-%d-description", it.pr.ID), it.pr.ID, 0, it.pr.Description)
			}
		case key.Matches(msg, m.keys.CreatePR):
			return m, m.startCreatePR()
		case key.Matches(msg, m.keys.DeclinePR):
			if it, ok := m.list.SelectedItem().(prItem); ok {
				m.pendingDeclinePRID = it.pr.ID
				m.mode = viewConfirmDecline
				return m, nil
			}
		case key.Matches(msg, m.keys.AddReviewer):
			if it, ok := m.list.SelectedItem().(prItem); ok {
				return m, editInTUI("add-reviewer",
					fmt.Sprintf("pr-%d-add-reviewer", it.pr.ID), it.pr.ID, 0,
					"# Enter one or more usernames (Server) or UUIDs/emails\n"+
						"# (Cloud), separated by space or comma. First non-comment\n"+
						"# line is used. Save & exit to submit; empty cancels.\n")
			}
		case key.Matches(msg, m.keys.RemoveReviewer):
			if it, ok := m.list.SelectedItem().(prItem); ok {
				return m, editInTUI("remove-reviewer",
					fmt.Sprintf("pr-%d-remove-reviewer", it.pr.ID), it.pr.ID, 0,
					reviewerListHint(it.pr.Reviewers))
			}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	m.updateDetail()
	return m, cmd
}

// reviewerListHint builds the seeded text shown in the remove-reviewer
// editor: a header explaining what to do plus one commented line per
// current reviewer so users can copy/paste a username instead of
// remembering it.
func reviewerListHint(rs []api.Reviewer) string {
	hint := ""
	for _, r := range rs {
		hint += "# " + r.Username
		if r.DisplayName != "" && r.DisplayName != r.Username {
			hint += "  (" + r.DisplayName + ")"
		}
		hint += "\n"
	}
	if hint == "" {
		hint = "# (no reviewers on this PR)\n"
	}
	return "# Enter one or more usernames/UUIDs to remove,\n" +
		"# separated by space or comma. Current reviewers:\n" +
		hint
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
	// When the PTY-embedded editor is active, carve out screen rows
	// for it BELOW the diff viewport (rendered as a fixed pane in
	// view.go). Without this the diff viewport stretches the full
	// height and the PTY pane drops off the bottom of the terminal.
	if m.pty != nil && m.pty.Active() && m.editorReturnTo == viewDiff {
		ptyH := m.pty.ChromeHeight() + 1 // +1 gap line between diff and pane
		m.diff.Height -= ptyH
		if m.diff.Height < 6 {
			m.diff.Height = 6
		}
	}
	m.comments.SetSize(m.width, m.height-helpHeight-2)
	m.settings.SetSize(m.width, m.height-helpHeight-4)
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

// openInBrowser, humanTime: file-local aliases so the dozens of TUI
// call sites stay terse. The single source of truth lives in
// internal/sysutil and internal/strutil respectively.
func openInBrowser(url string) error { return sysutil.OpenInBrowser(url) }

func humanTime(t time.Time) string { return strutil.HumanTime(t) }

// styleState is a thin alias for theme.StyleState so the in-file
// callers stay terse.
func styleState(s string) string { return theme.StyleState(s) }

// titleBar / status / chip aliases — the canonical definitions live
// in internal/tui/theme. The aliases below let prs.go's many existing
// call sites stay terse without cluttering every line with `theme.`.
// Wrapping the styles in functions (instead of `var foo = theme.Foo`)
// is necessary because theme.Apply rebinds the underlying style
// variables at runtime; a snapshot at package init would freeze the
// old colours.
func titleBar(section string, chips ...string) string {
	return theme.TitleBar(section, chips...)
}
func renderStatusLine(loading bool, sp, status string) string {
	return theme.RenderStatusLine(loading, sp, status)
}
func joinFooter(status, help string) string { return theme.JoinFooter(status, help) }

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
)


// editInlineInTUI is editInTUI's cousin for line-anchored comments —
// it carries path/line/side through to the result message. lineNo == 0
// means a file-level comment (no specific line). Now a thin shim
// over the unified editor flow in editor.go.
func editInlineInTUI(prID int, path string, lineNo int, side string) tea.Cmd {
	return requestEditInline(prID, path, lineNo, side)
}

// sanitizeForFilename is a thin alias for strutil.SanitizeForFilename,
// kept locally so the dozens of TUI callers don't need to import the
// utility package each.
func sanitizeForFilename(s string) string { return strutil.SanitizeForFilename(s) }

// splitFirstLineTokens reads the first non-blank, non-comment line of
// the editor buffer and returns its whitespace- and comma-separated
// tokens. Used to parse one or more reviewer usernames from a single
// editor session.
func splitFirstLineTokens(s string) []string {
	for _, line := range strings.Split(s, "\n") {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "<!--") {
			continue
		}
		fields := strings.FieldsFunc(l, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t'
		})
		out := make([]string, 0, len(fields))
		for _, f := range fields {
			if f != "" {
				out = append(out, f)
			}
		}
		return out
	}
	return nil
}

// (colorizeDiff removed — parseDiff produces both metadata and styled
// text in one pass.)
