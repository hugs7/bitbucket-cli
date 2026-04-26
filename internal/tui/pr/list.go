// Package pr — PR list page.
//
// The default view: a scrollable list of PRs on the left, the
// selected PR's detail on the right. List item rendering and the
// detail-pane refresh helper live here.
package pr

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/hugs7/bitbucket-cli/internal/api"

)

type prItem struct {
	pr         api.PullRequest
	buildState string // "" / SUCCESSFUL / INPROGRESS / FAILED / CANCELLED / PENDING
}

func (i prItem) FilterValue() string { return i.pr.Title }
func (i prItem) Title() string {
	if dot := buildDot(i.buildState); dot != "" {
		return fmt.Sprintf("%s #%d  %s", dot, i.pr.ID, i.pr.Title)
	}
	return fmt.Sprintf("#%d  %s", i.pr.ID, i.pr.Title)
}
func (i prItem) Description() string {
	return fmt.Sprintf("%s · %s → %s · %s", i.pr.State, i.pr.SourceRef, i.pr.TargetRef, i.pr.Author)
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

