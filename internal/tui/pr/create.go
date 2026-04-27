// Package pr — Create-PR flow.
//
// 'C' opens an autocomplete-driven huh form (source / target /
// title / description) instead of a vim-style template. The form
// runs synchronously via tea.Exec so it can take over the terminal
// while the user fills it in, then returns control to the PR list.
package pr

import (
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/charmbracelet/huh"
	tea "github.com/charmbracelet/bubbletea"
)

// currentGitBranch returns the current local git branch name (or "" on
// failure). Used to pre-fill the source branch when creating a PR.
func currentGitBranch() string {
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// remoteBranches returns the names of branches known to the local git
// repo: local heads plus remote-tracking branches with the remote
// prefix stripped. Used as huh's input autocomplete when creating a
// PR. Returns nil silently if git isn't available — the input then
// degrades to plain text entry.
func remoteBranches() []string {
	out, err := exec.Command("git", "for-each-ref",
		"--format=%(refname)",
		"refs/heads", "refs/remotes").Output()
	if err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	var branches []string
	for _, line := range strings.Split(string(out), "\n") {
		ref := strings.TrimSpace(line)
		var name string
		switch {
		case strings.HasPrefix(ref, "refs/heads/"):
			name = strings.TrimPrefix(ref, "refs/heads/")
		case strings.HasPrefix(ref, "refs/remotes/"):
			rest := strings.TrimPrefix(ref, "refs/remotes/")
			if i := strings.Index(rest, "/"); i > 0 {
				rest = rest[i+1:]
			} else {
				continue
			}
			if rest == "HEAD" {
				continue
			}
			name = rest
		default:
			continue
		}
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		branches = append(branches, name)
	}
	return branches
}

// createPRMsg is posted after the huh form completes (success or
// cancel). The Update handler decides whether to fire CreatePR or
// just return the user to the list.
type createPRMsg struct {
	source, target, title, body string
	cancelled                   bool
	err                         error
}

// createPRForm implements tea.ExecCommand so we can pause the parent
// program, run a huh form on the same terminal, and post results back
// via the standard message loop.
type createPRForm struct {
	source, target, title, body string
	branches                    []string

	stdin          io.Reader
	stdout, stderr io.Writer

	cancelled bool
	err       error
}

func (f *createPRForm) SetStdin(r io.Reader)  { f.stdin = r }
func (f *createPRForm) SetStdout(w io.Writer) { f.stdout = w }
func (f *createPRForm) SetStderr(w io.Writer) { f.stderr = w }

func (f *createPRForm) Run() error {
	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Source branch").Value(&f.source).
			Suggestions(f.branches).Validate(formNonEmpty),
		huh.NewInput().Title("Target branch").Value(&f.target).
			Suggestions(f.branches).Validate(formNonEmpty),
		huh.NewInput().Title("Title").Value(&f.title).Validate(formNonEmpty),
		huh.NewText().Title("Description (optional)").Value(&f.body),
	)).WithInput(f.stdin).WithOutput(f.stdout)

	if err := form.Run(); err != nil {
		// huh returns ErrUserAborted when the user hits ctrl+c / esc;
		// treat that as "cancelled" rather than a hard error so the
		// parent TUI can resume cleanly without a toast.
		if err == huh.ErrUserAborted {
			f.cancelled = true
			return nil
		}
		f.err = err
	}
	return nil
}

func formNonEmpty(s string) error {
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("required")
	}
	return nil
}

// startCreatePR launches the huh form via tea.Exec. The parent
// program is suspended for the duration so huh has the terminal to
// itself; once the user submits or aborts we post a createPRMsg
// back into the bubbletea event loop and the model handles the API
// call (or the cancel) like any other message.
func (m *model) startCreatePR() tea.Cmd {
	source := currentGitBranch()
	target := ""
	if r, err := m.svc.GetRepo(m.project, m.slug); err == nil {
		target = r.DefaultRef
	}
	form := &createPRForm{
		source:   source,
		target:   target,
		branches: remoteBranches(),
	}
	return tea.Exec(form, func(err error) tea.Msg {
		// Propagate any unexpected error from the harness itself
		// (terminal acquisition, etc.) — huh-internal errors are
		// captured in form.err.
		if err != nil {
			return createPRMsg{err: err}
		}
		return createPRMsg{
			source:    form.source,
			target:    form.target,
			title:     form.title,
			body:      form.body,
			cancelled: form.cancelled,
			err:       form.err,
		}
	})
}
