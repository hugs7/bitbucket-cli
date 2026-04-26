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
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
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
	InlineComment, ToggleSide key.Binding
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
		ToggleSide:    key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "toggle side (context lines)")),
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
		short: [][]key.Binding{{k.Up, k.Down, k.Enter, k.Diff, k.Comments, k.Approve, k.Merge, k.State, k.Help, k.Quit}},
		full: [][]key.Binding{
			{k.Up, k.Down, k.Enter, k.Diff, k.Open},
			{k.Approve, k.Unapprove, k.NeedsWork, k.Merge},
			{k.EditDesc, k.Comments, k.Refresh, k.State},
			{k.Help, k.Back, k.Quit},
		},
	}
}
func (k keyMap) viewerHelp() modeKeyMap {
	return modeKeyMap{
		short: [][]key.Binding{{k.Up, k.Down, k.InlineComment, k.ToggleSide, k.Back, k.Quit}},
		full: [][]key.Binding{
			{k.Up, k.Down, k.InlineComment, k.ToggleSide},
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

	// Diff navigation: parsed lines + cursor position so we can anchor
	// inline comments to the correct (path, side, line). The viewport
	// itself only knows how to scroll a string, so we re-render with a
	// row marker each time the cursor moves.
	diffLines  []diffLine
	diffCursor int
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

	return model{
		svc: svc, project: project, slug: slug,
		state:    "OPEN",
		list:     l,
		detail:   viewport.New(0, 0),
		diff:     viewport.New(0, 0),
		comments: cl,
		spinner:  sp,
		help:     help.New(),
		keys:     defaultKeys(),
		loading:  true,
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
		m.diffCursor = firstCommentableLine(m.diffLines)
		m.diff.SetContent(m.renderDiff())
		m.diff.GotoTop()
		m.ensureDiffCursorVisible()
		m.diffID = msg.id
		m.mode = viewDiff
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
			// Inline comments don't appear in the comments list view
			// (it filters to general activity comments), so we don't
			// re-fetch — just toast the result.
			return m, tea.Batch(m.spinner.Tick, m.doAction(label, false, func() error {
				_, err := m.svc.AddInlineComment(m.project, m.slug, prID, api.InlineCommentInput{
					Text: text, Path: path, Line: line, Side: side,
				})
				return err
			}))
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

		// Mode-independent keys come first.
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
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
			// Cursor-based navigation: we manage the highlighted line
			// ourselves so 'c' has a precise (path, side, line) anchor.
			// Up/down/k/j move by one row; pgup/pgdown by a viewport
			// page. 't' flips the side on a context line. 'c' opens
			// the editor for an inline comment.
			n := len(m.diffLines)
			switch {
			case key.Matches(msg, m.keys.Up):
				if m.diffCursor > 0 {
					m.diffCursor--
				}
				m.diff.SetContent(m.renderDiff())
				m.ensureDiffCursorVisible()
				return m, nil
			case key.Matches(msg, m.keys.Down):
				if m.diffCursor < n-1 {
					m.diffCursor++
				}
				m.diff.SetContent(m.renderDiff())
				m.ensureDiffCursorVisible()
				return m, nil
			case msg.String() == "pgup":
				step := m.diff.Height
				if step < 1 {
					step = 1
				}
				m.diffCursor -= step
				if m.diffCursor < 0 {
					m.diffCursor = 0
				}
				m.diff.SetContent(m.renderDiff())
				m.ensureDiffCursorVisible()
				return m, nil
			case msg.String() == "pgdown", msg.String() == " ":
				step := m.diff.Height
				if step < 1 {
					step = 1
				}
				m.diffCursor += step
				if m.diffCursor > n-1 {
					m.diffCursor = n - 1
				}
				m.diff.SetContent(m.renderDiff())
				m.ensureDiffCursorVisible()
				return m, nil
			case msg.String() == "g":
				m.diffCursor = 0
				m.diff.SetContent(m.renderDiff())
				m.ensureDiffCursorVisible()
				return m, nil
			case msg.String() == "G":
				m.diffCursor = n - 1
				m.diff.SetContent(m.renderDiff())
				m.ensureDiffCursorVisible()
				return m, nil
			case key.Matches(msg, m.keys.ToggleSide):
				if m.diffCursor < n {
					dl := &m.diffLines[m.diffCursor]
					if dl.side == "both" {
						dl.preferOld = !dl.preferOld
						side, lineNo := inlineSide(*dl)
						m.status = fmt.Sprintf("anchor → %s side L%d", side, lineNo)
					} else {
						m.status = "side toggle only applies to context lines"
					}
				}
				return m, nil
			case key.Matches(msg, m.keys.InlineComment):
				if m.diffCursor >= n {
					return m, nil
				}
				dl := m.diffLines[m.diffCursor]
				if !dl.commentable() {
					m.status = "no file/line under cursor — move to a +/-/context line"
					return m, nil
				}
				side, lineNo := inlineSide(dl)
				return m, editInlineInTUI(m.diffID, dl.path, lineNo, side)
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
	m.diff.Width = m.width
	m.diff.Height = m.height - helpHeight - 1
	m.comments.SetSize(m.width, m.height-helpHeight-2)
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
		if m.diffCursor < len(m.diffLines) {
			dl := m.diffLines[m.diffCursor]
			if dl.commentable() {
				side, lineNo := inlineSide(dl)
				anchor = fmt.Sprintf("  ·  %s:%d (%s)", dl.path, lineNo, side)
			}
		}
		header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")).
			Render(fmt.Sprintf("Diff · PR #%d%s", m.diffID, anchor))
		body = header + "\n" + m.diff.View()
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

// diffLine carries both the on-screen styled text and metadata so we
// know where to anchor an inline comment when the user presses 'c'.
type diffLine struct {
	styled    string
	path      string
	side      string // "new", "old", "" (header / hunk / unknown), or "both" (context)
	lineNo    int   // file line number on the new side (or sole side)
	oldNo     int   // for "both" rows, the line on the old side
	preferOld bool  // for "both" rows, anchor on the old side
}

// commentable reports whether this row can carry an inline comment.
func (d diffLine) commentable() bool { return d.path != "" && d.side != "" && d.lineNo > 0 }

// parseDiff walks a unified diff and produces one diffLine per text
// line, decorated with file/line/side metadata where applicable. The
// styling mirrors the previous colorizeDiff colours.
func parseDiff(diff string) []diffLine {
	add := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	del := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	hunk := lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	meta := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("13"))

	var out []diffLine
	var path string
	var oldLine, newLine int

	for _, line := range strings.Split(diff, "\n") {
		dl := diffLine{styled: line}
		switch {
		case strings.HasPrefix(line, "diff "):
			// "diff --git a/foo b/foo" — take the b/ side as the post-image path.
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				path = strings.TrimPrefix(parts[3], "b/")
			}
			dl.styled = meta.Render(line)
		case strings.HasPrefix(line, "+++ "):
			p := strings.TrimSpace(strings.TrimPrefix(line, "+++ "))
			p = strings.TrimPrefix(p, "b/")
			if p != "/dev/null" {
				path = p
			}
			dl.styled = meta.Render(line)
		case strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "index "):
			dl.styled = meta.Render(line)
		case strings.HasPrefix(line, "@@"):
			oldLine, newLine = parseHunkHeader(line)
			dl.styled = hunk.Render(line)
		case strings.HasPrefix(line, "+"):
			dl.styled = add.Render(line)
			dl.path, dl.side, dl.lineNo = path, "new", newLine
			newLine++
		case strings.HasPrefix(line, "-"):
			dl.styled = del.Render(line)
			dl.path, dl.side, dl.lineNo = path, "old", oldLine
			oldLine++
		case strings.HasPrefix(line, " "):
			// Context: exists on both sides. We default to "new" for
			// commenting; user can press 't' to flip to "old".
			dl.path, dl.side, dl.lineNo, dl.oldNo = path, "both", newLine, oldLine
			oldLine++
			newLine++
		case strings.HasPrefix(line, `\`):
			// "\ No newline at end of file" — leave as-is.
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

// renderDiff joins the parsed lines, prefixing the cursor row with a
// pointer so it's visually obvious where 'c' will land.
func (m *model) renderDiff() string {
	var b strings.Builder
	pointer := lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true).Render("▶ ")
	for i, dl := range m.diffLines {
		if i == m.diffCursor {
			b.WriteString(pointer)
		} else {
			b.WriteString("  ")
		}
		b.WriteString(dl.styled)
		b.WriteByte('\n')
	}
	return b.String()
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

// firstCommentableLine returns the first row that an inline comment
// can attach to, or 0 if none.
func firstCommentableLine(lines []diffLine) int {
	for i, dl := range lines {
		if dl.commentable() {
			return i
		}
	}
	return 0
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
