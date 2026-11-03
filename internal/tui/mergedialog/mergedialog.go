// Package mergedialog is the merge-confirmation card shared by the PR
// browser and the home dashboard: strategy picker, post-merge options,
// open tasks, and the merge itself.
package mergedialog

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hugs7/bitbucket-cli/internal/api"
	"github.com/hugs7/bitbucket-cli/internal/tui/theme"
)

// StrategiesLoadedMsg lands after Open has fetched the repo's allowed
// merge strategies; hosts pass it to New to show the dialog.
type StrategiesLoadedMsg struct {
	Project, Slug string
	PR            api.PullRequest
	Strategies    []api.MergeStrategy
	Err           error
}

// TasksLoadedMsg refreshes the open-tasks count shown in the dialog.
type TasksLoadedMsg struct {
	Project, Slug string
	PRID          int
	Tasks         []api.Task
	Err           error
}

// ResultMsg reports a finished merge. Retry is set when the merge was
// vetoed for missing approvals and can be repeated with merge checks
// temporarily bypassed; hosts must ask for consent before running it.
type ResultMsg struct {
	PRID  int
	Label string
	Err   error
	Retry tea.Cmd
}

// Model is the open merge dialog for one PR.
type Model struct {
	svc           api.Service
	project, slug string
	pr            api.PullRequest

	strategies         []api.MergeStrategy
	strategyIdx        int
	deleteBranch       bool
	retargetDependents bool
	bypassChecks       bool

	tasks        []api.Task
	tasksLoading bool
	tasksErr     error
	resolveTasks bool
}

// Open fetches the repo's merge strategies; the result lands as
// StrategiesLoadedMsg.
func Open(svc api.Service, project, slug string, pr api.PullRequest) tea.Cmd {
	return func() tea.Msg {
		ss, err := svc.MergeStrategies(project, slug)
		return StrategiesLoadedMsg{Project: project, Slug: slug, PR: pr, Strategies: ss, Err: err}
	}
}

// New builds the dialog with the repo default strategy selected and
// returns the command that loads the PR's open tasks.
func New(svc api.Service, msg StrategiesLoadedMsg) (*Model, tea.Cmd) {
	d := &Model{
		svc: svc, project: msg.Project, slug: msg.Slug, pr: msg.PR,
		strategies:         msg.Strategies,
		retargetDependents: true,
		tasksLoading:       true,
	}
	for i, st := range msg.Strategies {
		if st.Default {
			d.strategyIdx = i
			break
		}
	}
	project, slug, id := msg.Project, msg.Slug, msg.PR.ID
	return d, func() tea.Msg {
		ts, err := svc.ListTasks(project, slug, id)
		return TasksLoadedMsg{Project: project, Slug: slug, PRID: id, Tasks: ts, Err: err}
	}
}

// SetTasks applies a task fetch, ignoring responses for another PR.
func (d *Model) SetTasks(msg TasksLoadedMsg) {
	if msg.Project != d.project || msg.Slug != d.slug || msg.PRID != d.pr.ID {
		return
	}
	d.tasksLoading = false
	d.tasks = msg.Tasks
	d.tasksErr = msg.Err
	// Default "resolve all tasks before merge" on when there's
	// something to resolve; t flips it off if the user wants the veto.
	if msg.Err == nil && len(msg.Tasks) > 0 {
		d.resolveTasks = true
	}
}

// Update handles a key press. closed reports that the dialog should be
// dismissed; cmd is then the started merge, or nil if cancelled.
func (d *Model) Update(msg tea.KeyMsg) (closed bool, cmd tea.Cmd) {
	switch msg.String() {
	case "b":
		if _, ok := d.svc.(api.MergeCheckBypassService); ok {
			d.bypassChecks = !d.bypassChecks
		}
	case "d":
		d.deleteBranch = !d.deleteBranch
	case "r":
		d.retargetDependents = !d.retargetDependents
	case "t":
		// Inert without tasks so a stray press doesn't enable a no-op.
		if len(d.tasks) > 0 {
			d.resolveTasks = !d.resolveTasks
		}
	case "up", "k", "left", "h":
		if n := len(d.strategies); n > 0 {
			d.strategyIdx = (d.strategyIdx - 1 + n) % n
		}
	case "down", "j", "right", "l":
		if n := len(d.strategies); n > 0 {
			d.strategyIdx = (d.strategyIdx + 1) % n
		}
	case "y", "Y", "enter":
		return true, d.run()
	case "n", "N", "esc":
		return true, nil
	}
	return false, nil
}

// run resolves tasks if requested, merges, and deletes the source
// branch if requested, emitting a ResultMsg.
func (d *Model) run() tea.Cmd {
	svc, project, slug := d.svc, d.project, d.slug
	prID, src := d.pr.ID, d.pr.SourceRef
	del, retarget, bypass := d.deleteBranch, d.retargetDependents, d.bypassChecks
	resolveTasks := d.resolveTasks
	tasks := append([]api.Task(nil), d.tasks...)
	strategyID, strategyName := "", ""
	if d.strategyIdx >= 0 && d.strategyIdx < len(d.strategies) {
		strategyID = d.strategies[d.strategyIdx].ID
		strategyName = d.strategies[d.strategyIdx].Name
	}
	label := fmt.Sprintf("merged #%d", prID)
	if strategyName != "" {
		label = fmt.Sprintf("merged #%d (%s)", prID, strategyName)
	}
	if resolveTasks && len(tasks) > 0 {
		label = fmt.Sprintf("resolved %d task(s) + ", len(tasks)) + label
	}
	if del {
		label += fmt.Sprintf(" + deleted %s", src)
	}
	merge := func(bypass bool) error {
		_, err := api.MergePullRequest(svc, project, slug, prID, api.MergeOptions{
			StrategyID:         strategyID,
			RetargetDependents: retarget,
			BypassChecks:       bypass,
		})
		if err != nil {
			return err
		}
		if del && src != "" {
			// Best-effort: the merge already succeeded, so surface
			// the failure but don't undo the merge.
			if derr := svc.DeleteBranch(project, slug, src); derr != nil {
				return fmt.Errorf("merged but branch delete failed: %w", derr)
			}
		}
		return nil
	}
	return func() tea.Msg {
		// Resolve every open task first so blocker comments don't
		// veto the merge. Partial-resolve state is fine; the next
		// attempt sees fewer open tasks.
		if resolveTasks {
			for _, t := range tasks {
				if err := svc.ResolveTask(project, slug, prID, t.ID); err != nil {
					return ResultMsg{PRID: prID, Label: label, Err: fmt.Errorf("resolve task #%d: %w", t.ID, err)}
				}
			}
		}
		err := merge(bypass)
		result := ResultMsg{PRID: prID, Label: label, Err: err}
		if !bypass && api.IsBypassableApprovalVeto(svc, err) {
			result.Retry = func() tea.Msg {
				return ResultMsg{PRID: prID, Label: label + " (checks bypassed)", Err: merge(true)}
			}
		}
		return result
	}
}

// View draws the card sized for a terminal termW columns wide; spinner
// is shown while tasks load.
func (d *Model) View(termW int, spinner string) string {
	width := confirmWidth(termW)

	header := theme.TitleBadge.Render(fmt.Sprintf(" MERGE PR #%d ", d.pr.ID))
	switch {
	case d.pr.SourceRef != "" && d.pr.TargetRef != "":
		header += "  " + theme.TitleChipDim.Render(d.pr.SourceRef+" → "+d.pr.TargetRef)
	case d.pr.SourceRef != "":
		header += "  " + theme.TitleChipDim.Render(d.pr.SourceRef)
	}

	parts := []string{header, ""}

	parts = append(parts, theme.TitleChip.Render("Strategy"))
	if n := len(d.strategies); n == 0 {
		parts = append(parts, "  "+theme.TitleChipDim.Render("(no strategies available — using default)"))
	} else {
		idx := d.strategyIdx
		if idx < 0 || idx >= n {
			idx = 0
		}
		// Widest name first so descriptions line up in a column.
		nameW := 0
		for _, st := range d.strategies {
			if w := lipgloss.Width(st.Name); w > nameW {
				nameW = w
			}
		}
		for i, st := range d.strategies {
			parts = append(parts, strategyRow(i == idx, st, nameW))
		}
	}

	parts = append(parts, "", theme.TitleChip.Render("Options"))
	parts = append(parts, "  "+toggle(d.deleteBranch, "d",
		fmt.Sprintf("delete source branch %q after merge", d.pr.SourceRef)))
	parts = append(parts, "  "+toggle(d.retargetDependents, "r",
		"retarget dependent PRs to this PR's base branch"))
	if _, ok := d.svc.(api.MergeCheckBypassService); ok {
		parts = append(parts, "  "+toggle(d.bypassChecks, "b",
			"temporarily bypass merge checks / reviewer minimums (admin)"))
		if d.bypassChecks {
			parts = append(parts, theme.TitleChipWarn.Render("  Repository-wide: other PRs can merge during this window."),
				"  Settings restored after merge/failure. If interrupted, run:",
				"  bb pr recover-merge-checks -R "+d.svc.Host()+"/"+d.project+"/"+d.slug)
		}
	}

	hasTasks := len(d.tasks) > 0
	taskLabel := "resolve all open tasks before merging"
	if !hasTasks && !d.tasksLoading && d.tasksErr == nil {
		taskLabel += "  " + theme.TitleChipDim.Render("(none open)")
	}
	parts = append(parts, "  "+toggle(d.resolveTasks, "t", taskLabel))

	switch {
	case d.tasksLoading:
		parts = append(parts, "", theme.TitleChipDim.Render("  "+spinner+" loading tasks…"))
	case d.tasksErr != nil:
		parts = append(parts, "", lipgloss.NewStyle().Foreground(lipgloss.Color("9")).
			Render("  ✗ tasks: "+d.tasksErr.Error()))
	case hasTasks:
		parts = append(parts, "", theme.TitleChipWarn.Render(
			fmt.Sprintf("Open tasks: %d", len(d.tasks))))
		for _, t := range d.tasks {
			text := strings.TrimSpace(strings.SplitN(t.Text, "\n", 2)[0])
			if text == "" {
				text = "(no description)"
			}
			// Trim long task lines so they don't wrap in the box.
			max := width - 12
			if max > 0 && lipgloss.Width(text) > max {
				text = text[:max-1] + "…"
			}
			parts = append(parts, "  "+theme.TitleChipDim.Render("•")+
				" "+theme.TitleChipDim.Render(fmt.Sprintf("#%d", t.ID))+
				"  "+text)
		}
	}

	parts = append(parts, "",
		theme.TitleChipDim.Render(
			"↑/↓ strategy · d delete · r retarget · t tasks · b bypass · y merge · esc cancel"))

	box := lipgloss.NewStyle().
		Border(theme.Border()).
		BorderForeground(lipgloss.Color("10")).
		Padding(1, 2).
		Width(width)
	return box.Render(strings.Join(parts, "\n"))
}

// strategyRow draws one row in the strategy picker: a solid bar when
// selected, otherwise a muted glyph + name + git command hint.
func strategyRow(selected bool, st api.MergeStrategy, nameW int) string {
	hint := strategyHint(st.ID)
	tag := ""
	if st.Default {
		tag = "  " + theme.TitleChipDim.Render("(repo default)")
	}
	name := st.Name
	if pad := nameW - lipgloss.Width(name); pad > 0 {
		name += strings.Repeat(" ", pad)
	}

	if selected {
		line := "▸ ● " + name + "  " + hint + tag
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("231")).
			Background(lipgloss.Color("22")).
			Bold(true).
			Render(line)
	}
	return "  ○ " + name + "  " + theme.TitleChipDim.Render(hint) + tag
}

// toggle formats a "[x] k  label" line for the options pane.
func toggle(on bool, key, label string) string {
	check := "[ ]"
	if on {
		check = "[x]"
	}
	return check + " " + theme.TitleChipWarn.Render(key) + "  " + label
}

// strategyHint returns the git-command sketch for a known strategy ID,
// or "" if unknown.
func strategyHint(id string) string {
	switch id {
	case "no-ff", "merge_commit":
		return "--no-ff"
	case "ff", "fast_forward":
		return "--ff"
	case "ff-only":
		return "--ff-only"
	case "rebase-no-ff":
		return "rebase + merge --no-ff"
	case "rebase-ff-only":
		return "rebase + merge --ff-only"
	case "squash":
		return "--squash"
	case "squash-ff-only":
		return "--squash --ff-only"
	}
	return ""
}

// confirmWidth picks a roomy card width, capped for ultrawide terminals.
func confirmWidth(termW int) int {
	const minW, maxW = 60, 90
	w := termW - 4
	if w < minW {
		w = minW
	}
	if w > maxW {
		w = maxW
	}
	return w
}
