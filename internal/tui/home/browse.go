// Package home — Browse tab.
//
// The Browse tab is a debounced live-search across all repos with a
// README preview in the right pane. Everything browse-specific
// (search box, repoBrowseItem, fetch helpers) lives here.
package home

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/hugs7/bitbucket-cli/internal/api"
)

type repoBrowseItem struct{ r api.Repo }

func (i repoBrowseItem) FilterValue() string { return i.r.Name + " " + i.r.Slug + " " + i.r.Description }
func (i repoBrowseItem) Title() string {
	return fmt.Sprintf("%s/%s", i.r.Project, i.r.Slug)
}
func (i repoBrowseItem) Description() string {
	desc := i.r.Description
	if desc == "" {
		desc = "(no description)"
	}
	if len(desc) > 80 {
		desc = desc[:77] + "…"
	}
	return desc
}


// runSearchNow fires a search immediately (used for explicit Enter on
// the search input). Bumps the version so any in-flight debounced
// tick is invalidated. Returns immediately with cached results if we
// already searched for this query in this session.
func (m *homeModel) runSearchNow(q string) tea.Cmd {
	m.searchVersion++
	m.browseQ = q
	if cached, ok := m.searchCache[q]; ok {
		m.applyCachedResults(cached)
		return nil
	}
	m.loading = true
	m.searching = true
	m.browse.SetItems(nil)
	return tea.Batch(m.spinner.Tick, m.fetchRepos(q))
}

// applyCachedResults reseats the browse list from a cached slice
// without going through the API, then loads the README for whichever
// item happens to be selected.
func (m *homeModel) applyCachedResults(repos []api.Repo) tea.Cmd {
	items := make([]list.Item, 0, len(repos))
	for _, r := range repos {
		items = append(items, repoBrowseItem{r})
	}
	m.browse.SetItems(items)
	m.loading = false
	m.searching = false
	if len(items) == 0 {
		m.preview.SetContent(homeMuted.Render("No repos found in " + m.browseQ))
		return nil
	}
	m.browse.Select(0)
	if it, ok := items[0].(repoBrowseItem); ok {
		m.loading = true
		return tea.Batch(m.spinner.Tick, m.fetchReadme(it.r.Project, it.r.Slug))
	}
	return nil
}

// scheduleHotSearch schedules a debounced search 250ms after the last
// keystroke. The captured version is checked when the tick fires —
// only the latest scheduled tick wins, so rapid typing doesn't spam
// the API.
func (m *homeModel) scheduleHotSearch() tea.Cmd {
	m.searchVersion++
	v := m.searchVersion
	q := strings.TrimSpace(m.search.Value())
	return tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg {
		return searchTickMsg{q: q, version: v}
	})
}

func (m homeModel) fetchRepos(query string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		repos, err := svc.SearchRepos(query, 100)
		if err != nil {
			return homeErrMsg{err: err}
		}
		return reposLoadedMsg{repos: repos}
	}
}

func (m homeModel) fetchReadme(project, slug string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		body, err := svc.GetReadme(project, slug)
		if err != nil {
			return homeErrMsg{err: err}
		}
		return readmeLoadedMsg{project: project, slug: slug, body: body}
	}
}


// searchBox wraps the search text input (or its placeholder hint) in a
// bordered chrome with a focus colour so users can tell at a glance
// whether keystrokes are being captured.
func (m *homeModel) searchBox(innerW int) string {
	focused := m.search.Focused()
	c := lipgloss.Color("245")
	if focused {
		c = lipgloss.Color("57")
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(c).
		Padding(0, 1).
		Width(innerW - 2)

	var body string
	switch {
	case focused:
		body = m.search.View()
	case m.browseQ != "":
		body = "🔎 " + m.browseQ + homeMuted.Render("   (/ to search again · ctrl-u to clear)")
	default:
		body = homeMuted.Render("🔎 press / to search repos")
	}
	return box.Render(body)
}

