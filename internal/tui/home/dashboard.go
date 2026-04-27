// Package home — Dashboard tab.
//
// The Dashboard tab stacks four data sources (review queue, your
// authored PRs, recently closed PRs, recently viewed repos) and
// drives a single cursor across them. All dashboard-only state and
// rendering lives in this file.
package home

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/hugs7/bitbucket-cli/internal/api"
	"github.com/hugs7/bitbucket-cli/internal/strutil"
	"github.com/hugs7/bitbucket-cli/internal/tui/mdrender"
	"github.com/hugs7/bitbucket-cli/internal/tui/theme"
)

// dashRow describes one selectable row in the dashboard view. PR rows
// hold their owning repo so the right pane can show PR detail and
// Enter can drill into the repo's PR TUI.
type dashRow struct {
	kind    string // "pr" or "repo"
	pr      api.ReviewPR
	repo    api.Repo
	section string // section title, for context in the preview header
}

func (r dashRow) project() string {
	if r.kind == "pr" {
		return r.pr.Project
	}
	return r.repo.Project
}
func (r dashRow) slug() string {
	if r.kind == "pr" {
		return r.pr.Slug
	}
	return r.repo.Slug
}
func (r dashRow) identity() string {
	return r.kind + ":" + r.project() + "/" + r.slug() + ":" + fmt.Sprint(r.pr.PR.ID)
}

// dashSection is one labelled block in the dashboard.
type dashSection struct {
	title string
	rows  []dashRow
}

// dashboardSections returns the four sections in the order they're
// rendered. Sections with no rows still appear so the user can see
// they were considered (with an empty-state hint).
func (m *homeModel) dashboardSections() []dashSection {
	prRows := func(prs []api.ReviewPR, sec string) []dashRow {
		rows := make([]dashRow, 0, len(prs))
		for _, p := range prs {
			rows = append(rows, dashRow{kind: "pr", pr: p, section: sec})
		}
		return rows
	}
	repoRows := func(repos []api.Repo, sec string) []dashRow {
		rows := make([]dashRow, 0, len(repos))
		for _, r := range repos {
			rows = append(rows, dashRow{kind: "repo", repo: r, section: sec})
		}
		return rows
	}
	return []dashSection{
		{title: "Pull requests to review", rows: prRows(m.reviewPRs, "Pull requests to review")},
		{title: "Your pull requests", rows: prRows(m.authoredPRs, "Your pull requests")},
		{title: "Recently closed pull requests", rows: prRows(m.closedPRs, "Recently closed pull requests")},
		{title: "Recently viewed repositories", rows: repoRows(m.recentRepos, "Recently viewed repositories")},
	}
}

// dashFlatRows returns every selectable row across all sections in
// render order. Used by dashCursor arithmetic + preview lookup.
func (m *homeModel) dashFlatRows() []dashRow {
	var out []dashRow
	for _, sec := range m.dashboardSections() {
		out = append(out, sec.rows...)
	}
	return out
}

// selectedDashRow returns the row currently under the dashboard cursor,
// or nil if the dashboard is empty.
func (m *homeModel) selectedDashRow() *dashRow {
	rows := m.dashFlatRows()
	if len(rows) == 0 {
		return nil
	}
	if m.dashCursor < 0 {
		m.dashCursor = 0
	}
	if m.dashCursor >= len(rows) {
		m.dashCursor = len(rows) - 1
	}
	r := rows[m.dashCursor]
	return &r
}

// accentDelegate returns a default list delegate with the selected-row
// left accent bar tinted to match the home palette (indigo 57 + cyan
// 159) so a highlighted entry pops on every tab.

// dashboardKey handles j/k/up/down/g/G navigation on the dashboard
// tab. Movements skip nothing (every row is selectable) but the
// global cursor wraps at the start/end of the flat row list.
func (m *homeModel) dashboardKey(msg tea.KeyMsg) tea.Cmd {
	rows := m.dashFlatRows()
	if len(rows) == 0 {
		return nil
	}
	prev := m.dashCursor
	switch msg.String() {
	case "j", "down":
		m.dashCursor++
	case "k", "up":
		m.dashCursor--
	case "g", "home":
		m.dashCursor = 0
	case "G", "end":
		m.dashCursor = len(rows) - 1
	case "ctrl+d", "pgdown":
		m.dashCursor += 5
	case "ctrl+u", "pgup":
		m.dashCursor -= 5
	default:
		return nil
	}
	if m.dashCursor < 0 {
		m.dashCursor = 0
	}
	if m.dashCursor >= len(rows) {
		m.dashCursor = len(rows) - 1
	}
	if m.dashCursor == prev {
		return nil
	}
	// Refresh content so dashVP knows how many lines exist; without
	// this snapDashViewport's clamp against maxYOffset (computed
	// from the stale empty line buffer) sticks YOffset at 0.
	innerW := m.dashVP.Width
	if innerW == 0 {
		innerW = m.width
	}
	m.refreshDashContent(innerW)
	m.snapDashViewport()
	return m.refreshDashboardPreview(false)
}

// dashCursorLine returns the rendered-line index of the row currently
// under dashCursor, plus its height in lines. Mirrors the layout
// inside renderDashboard so we can snap the viewport without
// re-rendering the content.
func (m *homeModel) dashCursorLine() (line, height int) {
	globalIdx := 0
	currentLine := 0
	for i, sec := range m.dashboardSections() {
		if i > 0 {
			currentLine++ // blank line between sections
		}
		currentLine++ // section header
		if len(sec.rows) == 0 {
			currentLine++ // "(empty)" placeholder
			continue
		}
		for range sec.rows {
			if globalIdx == m.dashCursor {
				return currentLine, 2
			}
			currentLine += 2
			globalIdx++
		}
	}
	return -1, 0
}

// snapDashViewport adjusts dashVP.YOffset so the row under the
// cursor stays inside the visible window.
func (m *homeModel) snapDashViewport() {
	line, height := m.dashCursorLine()
	if line < 0 || m.dashVP.Height == 0 {
		return
	}
	off := m.dashVP.YOffset
	if line < off {
		off = line
	} else if line+height > off+m.dashVP.Height {
		off = line + height - m.dashVP.Height
	}
	if off < 0 {
		off = 0
	}
	m.dashVP.SetYOffset(off)
}

// afterDashLoad runs after one of the four section endpoints replies.
// It applies any pending cursor restore and refreshes the right pane
// so the very first load of the dashboard immediately shows the PR
// detail or README for the highlighted row.
func (m *homeModel) afterDashLoad() tea.Cmd {
	if m.restore != nil && m.restore.DashIdx >= 0 {
		rows := m.dashFlatRows()
		if m.restore.DashIdx < len(rows) {
			m.dashCursor = m.restore.DashIdx
		}
	}
	return m.refreshDashboardPreview(false)
}

// refreshDashboardPreview updates the right pane based on the row
// currently under the dashboard cursor. PR rows render PR detail
// inline; repo rows trigger a README fetch (skipped if the same row
// was already showing, unless force=true).
func (m *homeModel) refreshDashboardPreview(force bool) tea.Cmd {
	r := m.selectedDashRow()
	if r == nil {
		m.preview.SetContent(homeMuted.Render(
			"Loading dashboard… data fills in as each section replies."))
		m.prevDashRow = ""
		return nil
	}
	id := r.identity()
	if !force && id == m.prevDashRow {
		return nil
	}
	m.prevDashRow = id
	if r.kind == "pr" {
		m.preview.SetContent(m.renderPRDetail(r.pr))
		m.preview.GotoTop()
		return nil
	}
	// Repo row → README.
	m.loading = true
	return tea.Batch(m.spinner.Tick, m.fetchReadme(r.repo.Project, r.repo.Slug))
}

// renderPRDetail formats a single PR for the right pane (used by the
// dashboard PR sections).
func (m *homeModel) renderPRDetail(rp api.ReviewPR) string {
	p := rp.PR
	bold := lipgloss.NewStyle().Bold(true)
	branch := lipgloss.NewStyle().Foreground(lipgloss.Color("159"))
	author := lipgloss.NewStyle().Foreground(lipgloss.Color("213"))

	var sb strings.Builder
	fmt.Fprintf(&sb, "%s  %s\n", bold.Render(fmt.Sprintf("#%d", p.ID)), bold.Render(p.Title))
	fmt.Fprintf(&sb, "%s/%s · %s\n", rp.Project, rp.Slug, theme.StyleState(p.State))
	fmt.Fprintf(&sb, "%s · %s → %s\n",
		author.Render(p.Author), branch.Render(p.SourceRef), branch.Render(p.TargetRef))
	if !p.UpdatedAt.IsZero() {
		fmt.Fprintln(&sb, homeMuted.Render("updated "+strutil.HumanTime(p.UpdatedAt)))
	}
	if p.WebURL != "" {
		fmt.Fprintln(&sb, homeMuted.Render(p.WebURL))
	}
	if p.Description != "" {
		fmt.Fprintln(&sb)
		// PR descriptions on Bitbucket are markdown — render them
		// through the shared glamour wrapper so headings, lists,
		// fenced code blocks etc. look the same as a README in the
		// preview pane next door.
		sb.WriteString(mdrender.Render(p.Description, m.preview.Width))
	}
	return sb.String()
}


// dashboard rendering ---------------------------------------------

var (
	dashSecHeader = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("231")).Background(lipgloss.Color("57")).
			Padding(0, 1)
	dashSecCount = lipgloss.NewStyle().Foreground(lipgloss.Color("159"))
	dashRowIdle  = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).
			PaddingLeft(2)
	dashRowMeta = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

// dashRowSel is built per-call so it picks up theme.AccentRule()
// when the active palette changes (e.g. swapping into 3270, where
// the upper-eighth block becomes a plain ASCII '|').
func dashRowSel() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("231")).Bold(true).
		Border(lipgloss.Border{Left: theme.AccentRule()}, false, false, false, true).
		BorderForeground(lipgloss.Color("213")).
		PaddingLeft(1)
}

// renderDashboard renders the four stacked sections, drives the
// viewport with the rendered content, and snaps the viewport's
// vertical offset so the highlighted row stays visible. Returns the
// final viewport view (already clipped to innerH lines).
func (m *homeModel) renderDashboard(innerW, innerH int) string {
	sections := m.dashboardSections()

	// Show a centred loader card only on the very first paint, before
	// any of the four endpoints have replied.
	allEmpty := true
	for _, s := range sections {
		if len(s.rows) > 0 {
			allEmpty = false
			break
		}
	}
	if allEmpty && m.loading {
		body := homeLoadBanner.Render(m.spinner.View()+" loading dashboard…") +
			"\n\n" + homeMuted.Render("Fetching review queue, your PRs, recent activity, recent repos…")
		return card("57", innerW, innerH, body)
	}

	// Push the latest content into dashVP so View renders the right
	// rows AND so the next mouse-wheel / key event sees a populated
	// line list. Without this Update-side refresh the stored model's
	// dashVP.lines stays empty (View only mutates a throwaway copy)
	// and viewport.ScrollDown bails out with "len(lines)==0".
	m.refreshDashContent(innerW)
	return m.dashVP.View()
}

// refreshDashContent rebuilds the dashboard's flat row list and
// stores it on dashVP so wheel scrolling / cursor-snap math work
// against an up-to-date line buffer. Safe to call repeatedly — the
// viewport preserves YOffset across SetContent calls (clamping to
// the new max if the content shrank).
func (m *homeModel) refreshDashContent(innerW int) {
	sections := m.dashboardSections()
	var sb strings.Builder
	globalIdx := 0
	for i, sec := range sections {
		if i > 0 {
			sb.WriteString("\n")
		}
		count := len(sec.rows)
		var header string
		if theme.Mainframe() {
			// CICS-panel style: bright bold uppercase title flanked
			// by `===` rules, with a `(n)` count after the title.
			title := strings.ToUpper(sec.title)
			rule := strings.Repeat("=", 3)
			titleLine := lipgloss.NewStyle().Bold(true).
				Foreground(lipgloss.Color("14")).
				Render(rule + " " + title + " " + rule)
			header = titleLine + " " +
				dashSecCount.Render(fmt.Sprintf("(%d)", count))
		} else {
			header = dashSecHeader.Render(" "+sec.title+" ") + " " +
				dashSecCount.Render(fmt.Sprintf("(%d)", count))
		}
		sb.WriteString(header)
		sb.WriteString("\n")
		if count == 0 {
			sb.WriteString(dashRowMeta.Render("   (empty)"))
			sb.WriteString("\n")
			continue
		}
		for _, r := range sec.rows {
			selected := globalIdx == m.dashCursor
			sb.WriteString(m.renderDashRow(r, selected, innerW))
			sb.WriteString("\n")
			globalIdx++
		}
	}
	m.dashVP.SetContent(sb.String())
}

// renderDashRow renders a single dashboard row (PR or repo). The
// selected row gets a left accent bar; idle rows are dim with subtle
// metadata.
func (m *homeModel) renderDashRow(r dashRow, selected bool, innerW int) string {
	var title, meta string
	if r.kind == "pr" {
		p := r.pr.PR
		title = fmt.Sprintf("#%d  %s", p.ID, strutil.Truncate(p.Title, innerW-12))
		meta = fmt.Sprintf("%s/%s · %s · %s",
			r.pr.Project, r.pr.Slug, p.Author, strutil.HumanTime(p.UpdatedAt))
	} else {
		title = fmt.Sprintf("%s/%s", r.repo.Project, r.repo.Slug)
		desc := r.repo.Description
		if desc == "" {
			desc = "(no description)"
		}
		meta = strutil.Truncate(desc, innerW-6)
	}
	if selected {
		return dashRowSel().Render(title) + "\n" +
			dashRowMeta.PaddingLeft(3).Render(meta)
	}
	return dashRowIdle.Render(title) + "\n" +
		dashRowMeta.PaddingLeft(3).Render(meta)
}

