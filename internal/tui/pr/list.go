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
	"github.com/hugs7/bitbucket-cli/internal/tui/mdrender"
)

type prItem struct {
	pr         api.PullRequest
	buildState string // "" / SUCCESSFUL / INPROGRESS / FAILED / CANCELLED / PENDING

	// Stack metadata: position is 1-based depth in the stack (1 =
	// base), total is the stack size. Both 0 when the PR isn't part
	// of any multi-PR stack.
	stackPos   int
	stackTotal int
}

func (i prItem) FilterValue() string { return i.pr.Title }
func (i prItem) Title() string {
	prefix := ""
	if dot := buildDot(i.buildState); dot != "" {
		prefix = dot + " "
	}
	if i.stackTotal > 1 {
		// Position glyph: ▲ tip, ▼ base, │ middle. Followed by a
		// "n/total" count so users see depth at a glance.
		glyph := "│"
		switch i.stackPos {
		case i.stackTotal:
			glyph = "▲"
		case 1:
			glyph = "▼"
		}
		prefix += fmt.Sprintf("%s %d/%d  ", glyph, i.stackPos, i.stackTotal)
	}
	return fmt.Sprintf("%s#%d  %s", prefix, i.pr.ID, i.pr.Title)
}
func (i prItem) Description() string {
	return fmt.Sprintf("%s · %s → %s · %s", i.pr.State, i.pr.SourceRef, i.pr.TargetRef, i.pr.Author)
}


// stackPosition pairs a 1-based depth with the stack's total size so
// list items can render their "n/total" badge without re-running the
// stack algorithm on every keystroke.
type stackPosition struct{ pos, total int }

// jumpStack moves the list cursor to the next (delta=+1) or previous
// (delta=-1) PR in the same stack as the current selection. Returns
// true when a move happened, false when the current PR isn't part of
// a multi-PR stack or there's no neighbour in the requested
// direction (so the caller can fall through to default behaviour).
func (m *model) jumpStack(delta int) bool {
	cur, ok := m.list.SelectedItem().(prItem)
	if !ok || cur.stackTotal <= 1 {
		return false
	}
	target := cur.stackPos + delta
	if target < 1 || target > cur.stackTotal {
		return false
	}
	// Find the list item with the matching stack position whose other
	// stack members include `cur`. Equality on (stackTotal, source/target
	// chain) is implicit because stacks are computed once per fetch.
	for i, it := range m.list.Items() {
		pi, ok := it.(prItem)
		if !ok || pi.stackTotal != cur.stackTotal || pi.stackPos != target {
			continue
		}
		// Confirm same chain by checking adjacency: target == pos±1
		// already; verify the cur PR's source is in the same stack
		// by comparing target ref linkage.
		if delta > 0 && pi.pr.TargetRef != cur.pr.SourceRef {
			continue
		}
		if delta < 0 && cur.pr.TargetRef != pi.pr.SourceRef {
			continue
		}
		m.list.Select(i)
		m.updateDetail()
		return true
	}
	return false
}

// computeStackPositions runs api.ComputeStacks and flattens the
// result into a per-PR-id map. Singleton stacks are skipped so
// callers can `if sp, ok := ...; ok` to gate the badge display.
func computeStackPositions(prs []api.PullRequest) map[int]stackPosition {
	stacks := api.ComputeStacks(prs)
	out := make(map[int]stackPosition, len(prs))
	for _, s := range stacks {
		if !s.IsStacked() {
			continue
		}
		for i, p := range s.Items {
			out[p.ID] = stackPosition{pos: i + 1, total: len(s.Items)}
		}
	}
	return out
}

func (m *model) updateDetail() {
	it, ok := m.list.SelectedItem().(prItem)
	if !ok {
		// Empty list / no selection: leave the detail pane blank
		// rather than showing a stale PR's metadata. The action
		// panel is also suppressed because there's nothing to act on.
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
	if it.stackTotal > 1 {
		fmt.Fprintln(&sb, muted.Render(fmt.Sprintf("stack: position %d of %d  ·  ]/[ to navigate", it.stackPos, it.stackTotal)))
	}
	fmt.Fprintln(&sb, muted.Render(p.WebURL))

	// Reviewer status badge: shows the current user's review state
	// against this PR so own-vs-other and approved-vs-pending are
	// visible at a glance without paging through reviewers.
	if badge := m.reviewerBadge(p); badge != "" {
		fmt.Fprintln(&sb)
		sb.WriteString(badge)
	}

	if p.Description != "" {
		fmt.Fprintln(&sb)
		// PR descriptions on Bitbucket are markdown — same shared
		// glamour wrapper as README rendering in the home and repo
		// previews so the styling stays uniform across surfaces.
		sb.WriteString(mdrender.Render(p.Description, m.detail.Width))
	}

	m.detail.SetContent(sb.String())
}

// reviewerBadge renders a one-line chip describing the current user's
// review state for the given PR — author, approved, needs-work,
// pending, or "not a reviewer". Empty when the user isn't configured.
func (m *model) reviewerBadge(p api.PullRequest) string {
	if m.svc.Me() == "" {
		return ""
	}
	chip := lipgloss.NewStyle().Bold(true).Padding(0, 1)
	switch {
	case m.isOwnPR(p):
		return chip.Background(lipgloss.Color("13")).Foreground(lipgloss.Color("231")).
			Render(" YOUR PR ")
	}
	switch m.myReviewerStatus(p) {
	case "APPROVED":
		return chip.Background(lipgloss.Color("10")).Foreground(lipgloss.Color("231")).
			Render(" YOU APPROVED ")
	case "NEEDS_WORK":
		return chip.Background(lipgloss.Color("9")).Foreground(lipgloss.Color("231")).
			Render(" YOU FLAGGED NEEDS WORK ")
	case "UNAPPROVED":
		return chip.Background(lipgloss.Color("11")).Foreground(lipgloss.Color("0")).
			Render(" PENDING YOUR REVIEW ")
	}
	return ""
}



