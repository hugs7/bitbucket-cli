// Package home — shared home chrome.
//
// Pane borders, info cards, README header, and the tab strip — the
// visual scaffolding shared by every tab. Pulled into its own file so
// each tab stays focused on its own data and behaviour.
package home

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/hugs7/bitbucket-cli/internal/api"
	"github.com/hugs7/bitbucket-cli/internal/tui/preview"
	"github.com/hugs7/bitbucket-cli/internal/tui/theme"
)

// homeMuted aliases the canonical preview.Muted hint colour so home
// stays visually in sync with the repo TUI's placeholder text — both
// surfaces want the same dim grey for "(no README)" / "loading…"
// style hints, and centralising the style means a future palette
// tweak only has to be made in one place.
var homeMuted = preview.Muted

// homeLoadBanner is the prominent "we're working" pill shared by the
// loader cards (reviews loading, browse searching, etc).
var homeLoadBanner = lipgloss.NewStyle().Bold(true).
	Foreground(lipgloss.Color("231")).Background(lipgloss.Color("57")).
	Padding(0, 2)

// paneBorder returns a rounded-border style for a pane. The border
// colour shifts to indigo when the pane is "focused" (current tab) so
// the eye is drawn to the active region.
func paneBorder(focused bool, w, h int) lipgloss.Style {
	c := lipgloss.Color("238")
	if focused {
		c = lipgloss.Color("57")
	}
	s := lipgloss.NewStyle().
		Border(theme.Border()).
		BorderForeground(c).
		Width(w - 2).
		Height(h - 2)
	return s
}

// card renders a small bordered card (used for empty-state and loader
// messages) sized to fit the inner pane area.
func card(borderColor string, w, h int, body string) string {
	style := lipgloss.NewStyle().
		Border(theme.Border()).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(1, 2).
		Width(w - 4)
	rendered := style.Render(body)
	// Centre the card vertically inside the pane.
	pad := (h - lipgloss.Height(rendered)) / 2
	if pad < 0 {
		pad = 0
	}
	return strings.Repeat("\n", pad) + rendered
}

// readmeHeader builds the strip that prefixes a loaded README in the
// right pane: project/slug pill, default branch chip, web URL, and a
// hint line of available actions.
func (m *homeModel) readmeHeader(project, slug string) string {
	title := theme.TitleBadge.Render(fmt.Sprintf(" %s/%s ", project, slug))
	chips := []string{title}
	// Pull metadata out of whichever pane currently holds the repo.
	var repo api.Repo
	switch m.tab {
	case tabDashboard:
		if r := m.selectedDashRow(); r != nil && r.kind == "repo" {
			repo = r.repo
		}
	case tabFavourites:
		if it, ok := m.favs.SelectedItem().(repoBrowseItem); ok {
			repo = it.r
		}
	case tabBrowse:
		if it, ok := m.browse.SelectedItem().(repoBrowseItem); ok {
			repo = it.r
		}
	}
	if repo.DefaultRef != "" {
		chips = append(chips, theme.TitleSep, theme.TitleChip.Render(theme.BranchGlyph()+repo.DefaultRef))
	}
	if repo.Description != "" {
		chips = append(chips, theme.TitleSep, theme.TitleChipDim.Render(repo.Description))
	}
	header := strings.Join(chips, "")
	hint := homeMuted.Render("p → PRs  ·  o → browser  ·  f → favourite")
	if repo.WebURL != "" {
		hint = homeMuted.Render(repo.WebURL) + "\n" + hint
	}
	return header + "\n" + hint
}


// renderTabs draws the tab strip with a clear active-tab pill and a
// matching underline beneath it so the eye can immediately locate the
// current tab without re-reading colours.
func (m homeModel) renderTabs() string {
	// 3270 tabs: bright cyan bold + ALL CAPS, no background pill (no
	// CICS panel ever shipped a coloured chip — function-key labels
	// were just bright text on the dark screen). Keep the underline
	// rule so the active tab still has an unmistakable anchor.
	var active, inactive lipgloss.Style
	if theme.Mainframe() {
		active = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("14")).
			Padding(0, 1)
		inactive = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")).
			Padding(0, 1)
	} else {
		active = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("231")).
			Background(lipgloss.Color("57")).
			Padding(0, 2)
		inactive = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Padding(0, 2)
	}
	underlineStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("57"))
	if theme.Mainframe() {
		underlineStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	}

	rendered := make([]string, 0, len(allTabs))
	widths := make([]int, 0, len(allTabs))
	activeIdx := -1
	for i, t := range allTabs {
		label := t.name()
		if t == tabFavourites {
			n := len(m.favs.Items())
			if n > 0 {
				label = fmt.Sprintf("%s (%d)", label, n)
			}
		}
		if theme.Mainframe() {
			label = strings.ToUpper(label)
		}
		var s string
		if t == m.tab {
			s = active.Render(label)
			activeIdx = i
		} else {
			s = inactive.Render(label)
		}
		rendered = append(rendered, s)
		widths = append(widths, lipgloss.Width(s))
	}
	row := strings.Join(rendered, " ")

	// Build the underline: spaces under inactive tabs, ▔ under the
	// active one. A space joins each tab in the row, so the
	// underline mirrors that with a single-space gap.
	var ub strings.Builder
	for i, w := range widths {
		if i > 0 {
			ub.WriteString(" ")
		}
		if i == activeIdx {
			ub.WriteString(underlineStyle.Render(strings.Repeat(theme.ActiveTabUnderline(), w)))
		} else {
			ub.WriteString(strings.Repeat(" ", w))
		}
	}
	return row + "\n" + ub.String()
}

