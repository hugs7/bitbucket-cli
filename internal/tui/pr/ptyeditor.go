// Package pr — PTY-embedded inline editor.
//
// Spawns the user's configured editor ($EDITOR / VISUAL / config.Editor)
// inside a pseudo-terminal sized to fit between diff lines, then renders
// the editor's screen state inline within the diff view via vt10x's
// VT100/xterm emulator. Keystrokes flow from bubbletea into the PTY;
// when the editor process exits, the temp file's contents become the
// saved comment.
//
// This is a "real" editor inside the TUI — not a textarea overlay. Vim,
// neovim, helix, micro, etc. all work; ":w | q" in vim saves the
// comment.
package pr

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/creack/pty"
	"github.com/hinshun/vt10x"

	"github.com/hugs7/bitbucket-cli/internal/config"
	"github.com/hugs7/bitbucket-cli/internal/strutil"
	"github.com/hugs7/bitbucket-cli/internal/tui/theme"
)

// ptyEditor wraps an editor process running inside a pseudo-terminal.
// The TUI renders vt10x's screen state in place of the inline-diff
// textarea; keystrokes received by bubbletea while ptyEditor.Active is
// true are translated into PTY input bytes.
type ptyEditor struct {
	req     editorRequest
	tmpFile string

	cols int
	rows int

	tty  *os.File       // PTY master
	cmd  *exec.Cmd      // running editor
	term vt10x.Terminal // virtual terminal consuming PTY output

	// done is closed when the editor process exits so the Update
	// loop can finalise (read temp file, dispatch editorResultMsg).
	done   chan struct{}
	exitMu sync.Mutex
	exited bool
}

// ptyInlineRows / ptyInlineCols size the embedded editor pane
// relative to the surrounding diff viewport. The editor takes up to
// half the diff height (so context above + below the comment stays
// visible) and the full diff width minus a couple of columns of
// border / padding.
func ptyInlineRows(diffHeight int) int {
	r := diffHeight / 2
	if r < 8 {
		r = 8
	}
	if r > 18 {
		r = 18
	}
	return r
}

func ptyInlineCols(diffWidth int) int {
	c := diffWidth - 8 // marker (2) + border (2) + padding (2) + safety (2)
	if c < 30 {
		c = 30
	}
	return c
}

// ptyTickMsg drives the periodic re-render so the embedded editor's
// output stays fresh without us having to plumb a write-callback into
// vt10x. Fired by ptyTick().
type ptyTickMsg struct{}

// ptyExitedMsg is emitted by the goroutine watching the editor
// process. The temp-file read happens in Update so any I/O errors
// surface as a normal editor result.
type ptyExitedMsg struct {
	err error
}

// newPTYEditor spawns the configured editor on a temp file (seeded
// with req.initial / req.header) inside a PTY of the given size and
// starts the goroutines that pump output into vt10x and watch for
// process exit. Returns the editor plus a Cmd that should be batched
// into the same Update return so the first tick + first input lands
// after bubbletea has processed the open.
func newPTYEditor(req editorRequest, cols, rows int) (*ptyEditor, tea.Cmd, error) {
	if cols < 20 {
		cols = 20
	}
	if rows < 6 {
		rows = 6
	}
	hint := req.hint
	if hint == "" {
		hint = req.purpose
	}
	f, err := os.CreateTemp("", "bb-edit-*-"+strutil.SanitizeForFilename(hint)+".md")
	if err != nil {
		return nil, nil, fmt.Errorf("create temp: %w", err)
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
		return nil, nil, fmt.Errorf("no editor configured")
	}
	args := append(parts[1:], tmp)
	cmd := exec.Command(parts[0], args...)
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
	)
	tty, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		os.Remove(tmp)
		return nil, nil, fmt.Errorf("start pty: %w", err)
	}

	term := vt10x.New(vt10x.WithSize(cols, rows))
	pe := &ptyEditor{
		req:     req,
		tmpFile: tmp,
		cols:    cols,
		rows:    rows,
		tty:     tty,
		cmd:     cmd,
		term:    term,
		done:    make(chan struct{}),
	}

	// Drain PTY output into the virtual terminal in a goroutine so
	// vt10x's state always reflects the editor's screen. vt10x.Parse
	// blocks per chunk — runs until the PTY is closed (editor exit).
	go func() {
		_ = term.Parse(bufio.NewReader(tty))
	}()

	// Wait for the editor to exit, then signal done so Update can
	// emit ptyExitedMsg and read the temp file back.
	exitCh := make(chan error, 1)
	go func() {
		exitCh <- cmd.Wait()
		pe.exitMu.Lock()
		pe.exited = true
		pe.exitMu.Unlock()
		close(pe.done)
	}()

	// Fire the first tick straight away plus an exit-watch Cmd.
	startCmd := tea.Batch(ptyTick(), pe.waitExit(exitCh))
	return pe, startCmd, nil
}

// ptyTick returns a Cmd that fires ptyTickMsg after a short delay so
// the diff re-renders periodically while the editor is active. The
// Update handler chains a fresh tick on each receipt, creating a
// loop that stops automatically when the editor closes (we don't
// re-arm once Active() goes false).
func ptyTick() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg { return ptyTickMsg{} })
}

// waitExit returns a Cmd that blocks on the editor's Wait() result
// and emits ptyExitedMsg when it lands.
func (pe *ptyEditor) waitExit(ch <-chan error) tea.Cmd {
	return func() tea.Msg {
		err := <-ch
		return ptyExitedMsg{err: err}
	}
}

// Active reports whether the editor process is still running.
// Used by the renderer to decide whether to draw the PTY pane.
func (pe *ptyEditor) Active() bool {
	if pe == nil {
		return false
	}
	pe.exitMu.Lock()
	defer pe.exitMu.Unlock()
	return !pe.exited
}

// Resize forwards a window-size change to the PTY and the virtual
// terminal so the editor can reflow. Called when the surrounding
// diff width changes (terminal resize).
func (pe *ptyEditor) Resize(cols, rows int) {
	if cols < 20 {
		cols = 20
	}
	if rows < 6 {
		rows = 6
	}
	pe.cols, pe.rows = cols, rows
	_ = pty.Setsize(pe.tty, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	pe.term.Resize(cols, rows)
}

// Close kills the editor process (best-effort) and removes the temp
// file. Safe to call multiple times.
func (pe *ptyEditor) Close() {
	if pe == nil {
		return
	}
	if pe.cmd != nil && pe.cmd.Process != nil {
		_ = pe.cmd.Process.Kill()
	}
	if pe.tty != nil {
		_ = pe.tty.Close()
	}
	if pe.tmpFile != "" {
		_ = os.Remove(pe.tmpFile)
	}
}

// ReadResult reads the temp file (after the editor has exited) and
// strips any header prefix so the caller gets just the user's text.
// Removes the temp file as a side-effect — call exactly once.
func (pe *ptyEditor) ReadResult() (string, error) {
	if pe == nil || pe.tmpFile == "" {
		return "", nil
	}
	defer func() {
		_ = os.Remove(pe.tmpFile)
		pe.tmpFile = ""
	}()
	data, err := os.ReadFile(pe.tmpFile)
	if err != nil {
		return "", err
	}
	text := string(data)
	if pe.req.header != "" {
		text = strings.TrimPrefix(text, pe.req.header)
	}
	return text, nil
}

// SendKey translates a bubbletea KeyMsg into the byte sequence the
// PTY expects and writes it to the editor's stdin. Handles the
// common special keys (arrows, function keys, ctrl+letters); the
// default case forwards the key's runes verbatim.
func (pe *ptyEditor) SendKey(msg tea.KeyMsg) {
	if pe == nil || pe.tty == nil {
		return
	}
	bs := keyToBytes(msg)
	if len(bs) == 0 {
		return
	}
	_, _ = pe.tty.Write(bs)
}

// keyToBytes maps a bubbletea KeyMsg to the raw bytes a terminal
// would send for that key. Returns nil for keys with no equivalent
// (which are then silently dropped).
func keyToBytes(msg tea.KeyMsg) []byte {
	s := msg.String()
	switch s {
	case "enter":
		return []byte{'\r'}
	case "tab":
		return []byte{'\t'}
	case "shift+tab":
		return []byte("\x1b[Z")
	case "esc":
		return []byte{0x1b}
	case "backspace":
		return []byte{0x7f}
	case "delete":
		return []byte("\x1b[3~")
	case "up":
		return []byte("\x1b[A")
	case "down":
		return []byte("\x1b[B")
	case "right":
		return []byte("\x1b[C")
	case "left":
		return []byte("\x1b[D")
	case "home":
		return []byte("\x1b[H")
	case "end":
		return []byte("\x1b[F")
	case "pgup":
		return []byte("\x1b[5~")
	case "pgdown":
		return []byte("\x1b[6~")
	case "space":
		return []byte{' '}
	}
	// ctrl+<letter>: map to control byte (1..26).
	if strings.HasPrefix(s, "ctrl+") && len(s) == 6 {
		c := s[5]
		if c >= 'a' && c <= 'z' {
			return []byte{byte(c-'a') + 1}
		}
	}
	// alt+<letter>: ESC + letter (xterm convention).
	if strings.HasPrefix(s, "alt+") && len(s) == 5 {
		c := s[4]
		return []byte{0x1b, c}
	}
	// Function keys F1..F12.
	switch s {
	case "f1":
		return []byte("\x1bOP")
	case "f2":
		return []byte("\x1bOQ")
	case "f3":
		return []byte("\x1bOR")
	case "f4":
		return []byte("\x1bOS")
	case "f5":
		return []byte("\x1b[15~")
	case "f6":
		return []byte("\x1b[17~")
	case "f7":
		return []byte("\x1b[18~")
	case "f8":
		return []byte("\x1b[19~")
	case "f9":
		return []byte("\x1b[20~")
	case "f10":
		return []byte("\x1b[21~")
	case "f11":
		return []byte("\x1b[23~")
	case "f12":
		return []byte("\x1b[24~")
	}
	// Default: forward the rune(s).
	if len(msg.Runes) > 0 {
		return []byte(string(msg.Runes))
	}
	return nil
}

// View renders the editor's virtual screen as a string of rendered
// lines wrapped in a bordered card. Cursor (if visible) is shown by
// inverting the cell at the cursor position.
func (pe *ptyEditor) View(diffWidth int) string {
	if pe == nil {
		return ""
	}
	pe.term.Lock()
	defer pe.term.Unlock()

	cur := pe.term.Cursor()
	curVis := pe.term.CursorVisible()

	var lines []string
	for y := 0; y < pe.rows; y++ {
		var b strings.Builder
		for x := 0; x < pe.cols; x++ {
			g := pe.term.Cell(x, y)
			ch := g.Char
			if ch == 0 {
				ch = ' '
			}
			cell := renderGlyph(g, string(ch))
			if curVis && x == cur.X && y == cur.Y {
				cell = lipgloss.NewStyle().Reverse(true).Render(string(ch))
			}
			b.WriteString(cell)
		}
		lines = append(lines, b.String())
	}
	body := strings.Join(lines, "\n")

	header := theme.TitleBadge.Render(" "+ptyEditorLabel(pe.req)+" ") + "  " +
		theme.TitleChipDim.Render(":w to save · :q! to abort · live "+config.Get().EditorCmd())

	w := diffWidth - 4
	if w < 30 {
		w = 30
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("57")).
		Padding(0, 1).
		Width(w).
		Render(header + "\n" + body)
}

// renderGlyph applies the glyph's foreground / mode bits to the
// character. We translate vt10x's ANSI colour indices straight into
// lipgloss's 256-colour space and pick out bold + reverse, the two
// most common attributes editors lean on for syntax highlight + UI.
func renderGlyph(g vt10x.Glyph, ch string) string {
	st := lipgloss.NewStyle()
	const (
		attrReverse   = 1 << 0
		_             = 1 << 1 // underline
		attrBold      = 1 << 2
	)
	if g.FG.ANSI() {
		st = st.Foreground(lipgloss.Color(fmt.Sprintf("%d", g.FG)))
	}
	if g.BG.ANSI() {
		st = st.Background(lipgloss.Color(fmt.Sprintf("%d", g.BG)))
	}
	if g.Mode&attrBold != 0 {
		st = st.Bold(true)
	}
	if g.Mode&attrReverse != 0 {
		st = st.Reverse(true)
	}
	return st.Render(ch)
}

// ptyEditorLabel mirrors inlineEditor.label so the embedded editor
// header reads naturally regardless of which edit kicked it off.
func ptyEditorLabel(req editorRequest) string {
	switch req.purpose {
	case "edit-description":
		return fmt.Sprintf("EDIT DESCRIPTION · PR #%d", req.prID)
	case "create-pr":
		return "CREATE PR"
	case "add-comment", "add-comment-diff":
		return fmt.Sprintf("NEW COMMENT · PR #%d", req.prID)
	case "reply-comment", "reply-inline-comment":
		return fmt.Sprintf("REPLY TO #%d", req.commentID)
	case "edit-comment", "edit-comment-diff":
		return fmt.Sprintf("EDIT COMMENT #%d", req.commentID)
	case "add-inline-comment":
		if req.line == 0 {
			return fmt.Sprintf("FILE COMMENT · %s", req.path)
		}
		return fmt.Sprintf("INLINE COMMENT · %s:%d", req.path, req.line)
	}
	return strings.ToUpper(req.purpose)
}
