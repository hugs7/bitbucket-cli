// Package pr — Edit PR target-branch flow.
//
// 'T' opens a small huh form with a single autocompleted branch
// input pre-filled with the PR's current target. Submitting fires
// UpdatePRTarget on the API service via the standard action plumbing
// so the user sees a toast and the list refreshes.
package pr

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// editTargetMsg is posted after the huh form closes (success or
// cancel). The Update handler decides whether to fire UpdatePRTarget
// or just resume the parent TUI.
type editTargetMsg struct {
	prID      int
	target    string
	cancelled bool
	err       error
}

// editTargetForm implements tea.ExecCommand: pause the parent
// program, run the huh form, post results back via editTargetMsg.
type editTargetForm struct {
	prID     int
	target   string // pre-filled with the current target; user can edit
	branches []string

	stdin          io.Reader
	stdout, stderr io.Writer

	cancelled bool
	err       error
}

func (f *editTargetForm) SetStdin(r io.Reader)  { f.stdin = r }
func (f *editTargetForm) SetStdout(w io.Writer) { f.stdout = w }
func (f *editTargetForm) SetStderr(w io.Writer) { f.stderr = w }

func (f *editTargetForm) Run() error {
	// Match the create-PR form's keymap so the autocomplete UX is
	// consistent: tab completes, enter submits.
	keymap := huh.NewDefaultKeyMap()
	keymap.Input.AcceptSuggestion = key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "complete"),
	)
	keymap.Input.Next = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "next"),
	)

	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title(fmt.Sprintf("New target branch for PR #%d", f.prID)).
			Value(&f.target).
			Suggestions(f.branches).
			Validate(formNonEmpty),
	)).WithInput(f.stdin).WithOutput(f.stdout).WithKeyMap(keymap)

	if err := form.Run(); err != nil {
		if err == huh.ErrUserAborted {
			f.cancelled = true
			return nil
		}
		f.err = err
	}
	return nil
}

// startEditTarget launches the huh form via tea.Exec for the
// currently-selected PR. The parent program is suspended for the
// duration; on submit/cancel an editTargetMsg lands and the model
// fires UpdatePRTarget through the existing doAction plumbing.
func (m *model) startEditTarget(prID int, currentTarget string) tea.Cmd {
	form := &editTargetForm{
		prID:     prID,
		target:   currentTarget,
		branches: remoteBranches(),
	}
	return tea.Exec(form, func(err error) tea.Msg {
		if err != nil {
			return editTargetMsg{prID: prID, err: err}
		}
		return editTargetMsg{
			prID:      prID,
			target:    strings.TrimSpace(form.target),
			cancelled: form.cancelled,
			err:       form.err,
		}
	})
}
