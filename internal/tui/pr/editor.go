// Package tui — editor flow.
//
// bb supports two editor experiences for comments and PR descriptions:
//
//   - Full-screen ($EDITOR): the historical default. tea.ExecProcess
//     suspends the program, runs the user's $EDITOR (vim, helix, …) on
//     a temp file, then resumes and reads the result back.
//   - Inline (PIP): a charmbracelet/bubbles textarea rendered as an
//     overlay on top of the current view. The surrounding context
//     (PR list, diff, comments) stays visible — useful for quick
//     comments where launching vim feels heavyweight.
//
// The choice is driven by config.InlineEditor; F11 toggles between
// the two modes mid-edit so users aren't locked in.
package pr

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hugs7/bitbucket-cli/internal/config"
	"github.com/hugs7/bitbucket-cli/internal/tui/theme"
	"github.com/hugs7/bitbucket-cli/internal/strutil"
)

// editorRequest captures everything an edit needs: which workflow
// triggered it (purpose), the PR / comment IDs, and any inline-comment
// anchor. Carried both in wantEditMsg (request) and editorResultMsg
// (response) so the dispatch in Update can match purpose to action.
type editorRequest struct {
	purpose   string
	hint      string
	prID      int
	commentID int

	// Inline-comment anchor (purpose == "add-inline-comment").
	path string
	line int
	side string

	// Initial buffer contents pre-filled for the user.
	initial string

	// Header is a comment marker prepended to the temp file (and
	// stripped from the result) so the editor shows context. Inline
	// comments use it; description / generic edits leave it empty.
	header string
}

// wantEditMsg asks the model to start an edit for the given request.
// Update branches on config.InlineEditor: inline mode opens a textarea
// overlay; fullscreen mode shells out via runFullscreenEditor. Going
// through a message keeps every call site identical regardless of
// which mode is active.
type wantEditMsg struct{ req editorRequest }

// requestEdit returns a Cmd that fires wantEditMsg. Use this anywhere
// editInTUI was previously called.
func requestEdit(req editorRequest) tea.Cmd {
	return func() tea.Msg { return wantEditMsg{req: req} }
}

// requestEditDescription / requestEditComment / etc — convenience
// wrappers so call sites stay readable. These mirror the old
// editInTUI signature.
func requestEditPR(purpose, hint string, prID, commentID int, initial string) tea.Cmd {
	return requestEdit(editorRequest{
		purpose: purpose, hint: hint, prID: prID, commentID: commentID, initial: initial,
	})
}

// requestEditInline asks for an inline-comment edit anchored to a
// specific (path, line, side). Mirrors the old editInlineInTUI.
func requestEditInline(prID int, path string, lineNo int, side string) tea.Cmd {
	hint := fmt.Sprintf("pr-%d-inline-L%d-%s", prID, lineNo, side)
	if lineNo == 0 {
		hint = fmt.Sprintf("pr-%d-file-%s", prID, strutil.SanitizeForFilename(path))
	}
	header := fmt.Sprintf("<!-- inline comment on %s:%d (%s side) -->\n", path, lineNo, side)
	if lineNo == 0 {
		header = fmt.Sprintf("<!-- file-level comment on %s -->\n", path)
	}
	return requestEdit(editorRequest{
		purpose: "add-inline-comment",
		hint:    hint,
		prID:    prID,
		path:    path,
		line:    lineNo,
		side:    side,
		header:  header,
	})
}

// runFullscreenEditor spawns the user's $EDITOR on a temp file and
// emits editorResultMsg when it returns. Same shape as the old
// editInTUI / editInlineInTUI, generalised over editorRequest so both
// flows share one implementation.
func runFullscreenEditor(req editorRequest) tea.Cmd {
	hint := req.hint
	if hint == "" {
		hint = req.purpose
	}
	f, err := os.CreateTemp("", "bb-edit-*-"+strutil.SanitizeForFilename(hint)+".md")
	if err != nil {
		return func() tea.Msg { return editorResultFor(req, "", err) }
	}
	tmp := f.Name()
	if req.header != "" {
		_, _ = f.WriteString(req.header)
	}
	if req.initial != "" {
		_, _ = f.WriteString(req.initial)
	}
	f.Close()

	editor := config.Get().EditorCmd()
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		os.Remove(tmp)
		return func() tea.Msg { return editorResultFor(req, "", fmt.Errorf("no editor configured")) }
	}
	args := append(parts[1:], tmp)
	cmd := exec.Command(parts[0], args...)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		defer os.Remove(tmp)
		if err != nil {
			return editorResultFor(req, "", err)
		}
		data, rerr := os.ReadFile(tmp)
		text := string(data)
		if req.header != "" {
			text = strings.TrimPrefix(text, req.header)
		}
		return editorResultFor(req, text, rerr)
	})
}

// editorResultFor builds an editorResultMsg from a request + result,
// copying the dispatch metadata (purpose, prID, commentID, anchor)
// into the response so Update can route it to the right action.
func editorResultFor(req editorRequest, text string, err error) editorResultMsg {
	return editorResultMsg{
		purpose:   req.purpose,
		prID:      req.prID,
		commentID: req.commentID,
		text:      text,
		err:       err,
		path:      req.path,
		line:      req.line,
		side:      req.side,
	}
}

// inlineEditor is the in-process textarea overlay. Wraps
// bubbles/textarea with the dispatch metadata so save / cancel /
// promote-to-fullscreen all know which request they're completing.
type inlineEditor struct {
	ta  textarea.Model
	req editorRequest
	// promoting is set when the user pressed F11 — the next pass
	// through Update flushes the buffer to runFullscreenEditor.
	promoting bool
}

// newInlineEditor constructs the textarea seeded with req.initial and
// sized roughly to the current terminal. The caller (model) is
// responsible for calling .layout() to refine the size.
func newInlineEditor(req editorRequest, w, h int) inlineEditor {
	ta := textarea.New()
	ta.Placeholder = "Type your text — ctrl+s to save · esc to cancel · F11 for $EDITOR"
	ta.Prompt = "│ "
	ta.CharLimit = 0
	ta.ShowLineNumbers = false
	if req.initial != "" {
		ta.SetValue(req.initial)
	}
	ta.SetWidth(editorBoxInnerWidth(w))
	ta.SetHeight(editorBoxInnerHeight(h))
	ta.Focus()
	return inlineEditor{ta: ta, req: req}
}

// editorBoxInnerWidth/Height compute the inner area of the centred
// overlay. We borrow ~70% of the terminal width and 60% of the
// height, with sensible floors so it stays usable on small windows.
func editorBoxInnerWidth(w int) int {
	v := (w * 70) / 100
	if v < 40 {
		v = 40
	}
	if v > w-4 {
		v = w - 4
	}
	return v - 4 // border + padding
}

func editorBoxInnerHeight(h int) int {
	v := (h * 60) / 100
	if v < 8 {
		v = 8
	}
	if v > h-4 {
		v = h - 4
	}
	return v - 4
}

// view renders the textarea as a centred bordered card overlaid on
// top of the existing TUI body. We use lipgloss.Place for centring;
// the caller composes the final string by passing this on top of the
// underlying view (typically replacing the body for that frame).
func (e inlineEditor) view(width, height int, label string) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("57")).
		Padding(1, 2).
		Width(editorBoxInnerWidth(width) + 2)

	header := theme.TitleBadge.Render(" " + label + " ") + "  " +
		theme.TitleChipDim.Render("ctrl+s save · esc cancel · F11 → $EDITOR")
	body := header + "\n\n" + e.ta.View()

	return lipgloss.Place(width, height,
		lipgloss.Center, lipgloss.Center,
		box.Render(body),
	)
}

// labelFor produces the human-readable badge text for the editor
// overlay (e.g. "EDIT DESCRIPTION", "REPLY TO #42").
func (e inlineEditor) label() string {
	switch e.req.purpose {
	case "edit-description":
		return fmt.Sprintf("EDIT DESCRIPTION · PR #%d", e.req.prID)
	case "create-pr":
		return "CREATE PR"
	case "add-comment", "add-comment-diff":
		return fmt.Sprintf("NEW COMMENT · PR #%d", e.req.prID)
	case "reply-comment", "reply-inline-comment":
		return fmt.Sprintf("REPLY TO #%d", e.req.commentID)
	case "edit-comment", "edit-comment-diff":
		return fmt.Sprintf("EDIT COMMENT #%d", e.req.commentID)
	case "add-inline-comment":
		if e.req.line == 0 {
			return fmt.Sprintf("FILE COMMENT · %s", e.req.path)
		}
		return fmt.Sprintf("INLINE COMMENT · %s:%d", e.req.path, e.req.line)
	case "add-reviewer":
		return "ADD REVIEWER"
	case "remove-reviewer":
		return "REMOVE REVIEWER"
	}
	return strings.ToUpper(e.req.purpose)
}

// handleEditorKey is wired in from prs.go's Update tea.KeyMsg branch
// when m.mode == viewEditor. It owns every keystroke while the
// overlay is active so global keys (esc/quit) don't accidentally
// discard a draft.
func (m model) handleEditorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+s", "alt+enter":
		text := m.editor.ta.Value()
		req := m.editor.req
		m.closeEditor()
		return m, func() tea.Msg { return editorResultFor(req, text, nil) }
	case "esc", "ctrl+c":
		req := m.editor.req
		m.closeEditor()
		// Empty text + nil err keeps the existing "aborted (empty)"
		// path in the editorResultMsg handler — no need for a
		// separate cancel signal.
		return m, func() tea.Msg { return editorResultFor(req, "", nil) }
	case "f11", "ctrl+\\":
		// Promote: hand the current buffer to $EDITOR via the same
		// fullscreen path. The original initial text is replaced
		// with the user's in-progress draft so nothing is lost.
		req := m.editor.req
		req.initial = m.editor.ta.Value()
		m.closeEditor()
		return m, runFullscreenEditor(req)
	}
	var cmd tea.Cmd
	m.editor.ta, cmd = m.editor.ta.Update(msg)
	return m, cmd
}

// closeEditor wipes the inline editor state and restores the prior
// view mode. Safe to call even if no editor is active.
func (m *model) closeEditor() {
	m.editorActive = false
	if m.editorReturnTo != viewEditor {
		m.mode = m.editorReturnTo
	} else {
		m.mode = viewList
	}
}
