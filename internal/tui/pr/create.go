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
	"strconv"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/hugs7/bitbucket-cli/internal/api"
	"github.com/hugs7/bitbucket-cli/internal/gitctx"
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

// BranchToTitle turns a git branch name into a sensible default PR
// title. Examples:
//
//	feature/some-feature       → "Feature: Some feature"
//	bugfix/fix_the_thing       → "Bugfix: Fix the thing"
//	hotfix/JIRA-123-broken     → "Hotfix: JIRA 123 broken"
//	some-feature               → "Some feature"
//
// The first path segment becomes the prefix (capitalised, followed by
// ": "). Remaining segments are merged into a single sentence with
// '-' / '_' / '/' treated as word separators. Existing letter capitalisation
// inside tokens (e.g. "JIRA") is preserved — we only force the
// first character of the prefix and the body to uppercase.
func BranchToTitle(branch string) string {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return ""
	}
	var prefix, rest string
	if i := strings.Index(branch, "/"); i > 0 {
		prefix = branch[:i]
		rest = branch[i+1:]
	} else {
		rest = branch
	}
	rest = strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || r == '/' {
			return ' '
		}
		return r
	}, rest)
	// Collapse runs of whitespace introduced by adjacent separators
	// (e.g. "foo--bar" → "foo  bar" → "foo bar").
	rest = strings.Join(strings.Fields(rest), " ")
	rest = upperFirst(rest)
	if prefix == "" {
		return rest
	}
	return upperFirst(prefix) + ": " + rest
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// remoteBranches returns the names of branches known to the local git
// repo: local heads plus remote-tracking branches with the remote
// prefix stripped. Used as huh's input autocomplete when creating a
// PR. Returns nil silently if git isn't available — the input then
// degrades to plain text entry.
func remoteBranches() []string {
	branches, _ := gitctx.TargetBranches("", "", "")
	return branches
}

// createPRMsg is posted after the huh form completes (success or
// cancel). The Update handler decides whether to fire CreatePR or
// just return the user to the list.
type createPRMsg struct {
	source, target, title, body string
	reviewers                   []string
	cancelled                   bool
	err                         error
}

// createPRForm implements tea.ExecCommand so we can pause the parent
// program, run a huh form on the same terminal, and post results back
// via the standard message loop.
type createPRForm struct {
	source, target, title, body string
	reviewers                   []string
	svc                         api.Service
	project, slug               string
	// titleHint is shown as the title input's placeholder so the
	// user sees the suggested title without it sitting in the
	// field as text they'd have to clear before retyping. If they
	// submit without typing anything, we adopt titleHint as the
	// final title.
	titleHint string
	branches  []string
	local     bool

	stdin          io.Reader
	stdout, stderr io.Writer

	cancelled bool
	err       error
}

func (f *createPRForm) SetStdin(r io.Reader)  { f.stdin = r }
func (f *createPRForm) SetStdout(w io.Writer) { f.stdout = w }
func (f *createPRForm) SetStderr(w io.Writer) { f.stderr = w }

func (f *createPRForm) Run() error {
	// Default huh keymap binds tab to "next field" and ctrl+e to
	// "accept suggestion". For a branch-name autocomplete that's
	// backwards from every shell anyone has ever used: tab should
	// commit the highlighted suggestion. We swap so:
	//   tab          → accept the autocomplete suggestion
	//   enter        → next field / submit
	//   shift+tab    → previous field
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
		huh.NewInput().Title("Source branch").Value(&f.source).
			Suggestions(f.branches).Validate(formNonEmpty),
		NewTargetInput(&f.source, &f.target, func(source string) []string {
			if !f.local {
				return nil
			}
			branches, _ := gitctx.TargetBranches("", "", gitctx.CanonicalBranchName(source, f.branches))
			return branches
		}),
		// Title shows the branch-derived suggestion as a
		// placeholder rather than pre-filled text — typing
		// replaces it without the user having to clear the
		// field first. PlaceholderFunc is re-evaluated whenever
		// f.source changes so picking a different source branch
		// instantly updates the suggested title. Empty
		// submissions fall back to the live hint after the form
		// returns, so the validator only checks that we have
		// *some* title (hint or typed).
		huh.NewInput().Title("Title").Value(&f.title).
			PlaceholderFunc(func() string {
				f.titleHint = BranchToTitle(f.source)
				return f.titleHint
			}, &f.source).
			Validate(func(s string) error {
				if strings.TrimSpace(s) == "" && strings.TrimSpace(f.titleHint) == "" {
					return fmt.Errorf("required")
				}
				return nil
			}),
		huh.NewText().Title("Description (optional)").Value(&f.body),
	)).WithInput(f.stdin).WithOutput(f.stdout).WithKeyMap(keymap)

	if err := form.Run(); err != nil {
		// huh returns ErrUserAborted when the user hits ctrl+c / esc;
		// treat that as "cancelled" rather than a hard error so the
		// parent TUI can resume cleanly without a toast.
		if err == huh.ErrUserAborted {
			f.cancelled = true
			return nil
		}
		f.err = err
		return nil
	}

	f.source = gitctx.CanonicalBranchName(f.source, f.branches)
	f.target = gitctx.CanonicalBranchName(f.target, f.branches)
	// Adopt the placeholder title when the user accepted it by
	// hitting enter without typing — the placeholder is purely
	// visual in huh, so f.title is still empty at this point.
	if strings.TrimSpace(f.title) == "" {
		f.title = f.titleHint
	}

	// After the form succeeds, sanity-check that the source branch
	// actually exists on the remote (and is up to date) — otherwise
	// the PR would be opened against a ref the server hasn't seen
	// yet, or against a stale tip that's missing local commits.
	// Each prompt is independent: the user can decline and we'll
	// still hand the createPRMsg back so they can continue (e.g.
	// they pushed manually in another terminal).
	f.syncSourceBranch(keymap)
	if f.err != nil || f.cancelled {
		return nil
	}
	f.reviewers, f.err = ChooseReviewers(f.svc, f.project, f.slug, f.source, f.target, nil, f.stdin, f.stdout)
	if f.err == huh.ErrUserAborted {
		f.cancelled = true
		f.err = nil
	}
	return nil
}

// syncSourceBranch is invoked after the main create-PR form. It
// inspects the local git state for f.source and prompts the user to
// publish or push it as needed. Errors are surfaced via f.err so the
// parent TUI shows them instead of silently submitting against a
// missing/outdated ref.
func (f *createPRForm) syncSourceBranch(keymap *huh.KeyMap) {
	if !f.local {
		return
	}
	branch := strings.TrimSpace(f.source)
	if branch == "" {
		return
	}
	repo, err := gitctx.Current("")
	if err != nil {
		f.err = err
		return
	}
	upstream := "refs/remotes/" + repo.RemoteName + "/" + branch
	_, existsErr := exec.Command("git", "rev-parse", "--verify", upstream).Output()
	if existsErr != nil {
		// The selected Bitbucket remote has no known tracking ref.
		// Do not alter an existing upstream on another host.
		var push bool
		prompt := huh.NewForm(huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Branch %q is not tracked on %s.", branch, repo.RemoteName)).
				Description("Push it now so the PR can target the remote ref?").
				Affirmative("Yes, push").Negative("No, skip").
				Value(&push),
		)).WithInput(f.stdin).WithOutput(f.stdout).WithKeyMap(keymap)
		if err := prompt.Run(); err != nil {
			if err == huh.ErrUserAborted {
				f.cancelled = true
				return
			}
			f.err = err
			return
		}
		if push {
			if err := gitPushBranch(repo.RemoteName, branch, f.stdout, f.stderr); err != nil {
				f.err = fmt.Errorf("push %q failed: %w", branch, err)
			}
		}
		return
	}

	// Upstream exists — check whether the local tip has commits
	// the remote tip doesn't, and offer to push them.
	ahead, err := branchAhead(branch, upstream)
	if err != nil || ahead == 0 {
		return
	}
	commits := "commit"
	if ahead != 1 {
		commits = "commits"
	}
	var push bool
	prompt := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title(fmt.Sprintf("Branch %q is ahead of %s by %d %s.", branch, upstream, ahead, commits)).
			Description("Push the unpushed commits before opening the PR?").
			Affirmative("Yes, push").Negative("No, skip").
			Value(&push),
	)).WithInput(f.stdin).WithOutput(f.stdout).WithKeyMap(keymap)
	if err := prompt.Run(); err != nil {
		if err == huh.ErrUserAborted {
			f.cancelled = true
			return
		}
		f.err = err
		return
	}
	if push {
		if err := gitPushBranch(repo.RemoteName, branch, f.stdout, f.stderr); err != nil {
			f.err = fmt.Errorf("push %q failed: %w", branch, err)
		}
	}
}

// branchAhead returns how many commits branch has that upstream
// doesn't. Zero means the branch is up to date (or behind, which we
// don't try to fix here — that's the user's problem to rebase).
func branchAhead(branch, upstream string) (int, error) {
	out, err := exec.Command("git", "rev-list", "--count",
		upstream+".."+branch).Output()
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}

// gitPushBranch publishes the chosen branch to the selected Bitbucket remote,
// without changing an upstream that may intentionally point at another host.
func gitPushBranch(remote, branch string, stdout, stderr io.Writer) error {
	cmd := exec.Command("git", "push", "--", remote, "refs/heads/"+branch+":refs/heads/"+branch)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
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
	local := gitctx.Matches(m.svc.Host(), m.project, m.slug)
	var source string
	var branches []string
	if local {
		source = currentGitBranch()
		branches = remoteBranches()
	}
	form := &createPRForm{
		svc: m.svc, project: m.project, slug: m.slug,
		source:    source,
		titleHint: BranchToTitle(source),
		branches:  branches,
		local:     local,
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
			reviewers: form.reviewers,
			cancelled: form.cancelled,
			err:       form.err,
		}
	})
}
