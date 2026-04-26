// Package tui — home page model.
//
// The home model is the entry point you get from running `bb` with no
// args: a two-pane dashboard with tabs for "Reviews" (PRs assigned to
// you across hosts) and "Browse" (search a project / workspace and
// drill into its repos to see the README, then jump into the PR TUI).
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hugs7/bitbucket-cli/internal/api"
)

// HomeAction is what Home returns to its caller when the user picks an
// action that requires launching a different TUI program (e.g. "open
// the PR browser for this repo"). nil means "user quit, we're done".
type HomeAction struct {
	Kind    string // "prs"
	Project string
	Slug    string
}

// Home launches the dashboard. It returns the next action the caller
// should run (or nil if the user just quit).
func Home(svc api.Service) (*HomeAction, error) {
	m := newHomeModel(svc)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	final, err := p.Run()
	if err != nil {
		return nil, err
	}
	hm := final.(homeModel)
	if hm.next != nil {
		return hm.next, nil
	}
	return nil, nil
}

type homeTab int

const (
	tabReviews homeTab = iota
	tabBrowse
)

type reviewItem struct{ r api.ReviewPR }

func (i reviewItem) FilterValue() string { return i.r.PR.Title + " " + i.r.Project + "/" + i.r.Slug }
func (i reviewItem) Title() string {
	return fmt.Sprintf("#%d  %s", i.r.PR.ID, i.r.PR.Title)
}
func (i reviewItem) Description() string {
	return fmt.Sprintf("%s/%s · %s · %s",
		i.r.Project, i.r.Slug, i.r.PR.State, i.r.PR.Author)
}

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

type homeKeys struct {
	Up, Down, Enter, Tab, Search, Open, Quit, Back, Help, OpenPRs key.Binding
}

func defaultHomeKeys() homeKeys {
	return homeKeys{
		Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Enter:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open / load")),
		Tab:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch tab")),
		Search:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		Open:    key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "browser")),
		OpenPRs: key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "open PRs")),
		Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back / blur")),
		Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	}
}

func (k homeKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Enter, k.Tab, k.Search, k.OpenPRs, k.Open, k.Help, k.Quit}
}
func (k homeKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Enter, k.Tab, k.Search},
		{k.OpenPRs, k.Open, k.Help, k.Back, k.Quit},
	}
}

type homeModel struct {
	svc api.Service

	tab    homeTab
	keys   homeKeys
	help   help.Model
	width  int
	height int

	reviews  list.Model
	browse   list.Model
	search   textinput.Model
	preview  viewport.Model
	spinner  spinner.Model
	loading  bool
	status   string
	err      error
	browseQ  string // last search executed (workspace / project key)

	next *HomeAction
}

type reviewsLoadedMsg struct{ prs []api.ReviewPR }
type reposLoadedMsg struct{ repos []api.Repo }
type readmeLoadedMsg struct {
	project, slug string
	body          string
}
type homeErrMsg struct{ err error }

func newHomeModel(svc api.Service) homeModel {
	rl := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	rl.Title = "PRs awaiting your review"
	rl.SetShowHelp(false)
	rl.SetShowStatusBar(true)

	bl := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	bl.Title = "Browse repos"
	bl.SetShowHelp(false)

	ti := textinput.New()
	ti.Placeholder = "workspace or project key, e.g. myteam"
	ti.Prompt = "🔎 "
	ti.CharLimit = 80

	pv := viewport.New(0, 0)

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	return homeModel{
		svc:     svc,
		tab:     tabReviews,
		keys:    defaultHomeKeys(),
		help:    help.New(),
		reviews: rl,
		browse:  bl,
		search:  ti,
		preview: pv,
		spinner: sp,
		loading: true,
	}
}

func (m homeModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.fetchReviews())
}

func (m homeModel) fetchReviews() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		prs, err := svc.ListMyReviewPRs(50)
		if err != nil {
			return homeErrMsg{err: err}
		}
		return reviewsLoadedMsg{prs: prs}
	}
}

func (m homeModel) fetchRepos(project string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		repos, err := svc.ListRepos(project, 100)
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

func (m *homeModel) layout() {
	helpH := lipgloss.Height(m.help.View(m.keys))
	contentH := m.height - helpH - 3 // header + tabs + footer

	leftW := m.width / 2
	if leftW < 30 {
		leftW = m.width
	}
	rightW := m.width - leftW - 2

	m.reviews.SetSize(leftW, contentH)
	m.browse.SetSize(leftW, contentH-2) // leave room for search input
	m.search.Width = leftW - 4
	m.preview.Width = rightW
	m.preview.Height = contentH
}

func (m homeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.loading {
			return m, cmd
		}
		return m, nil

	case reviewsLoadedMsg:
		m.loading = false
		items := make([]list.Item, 0, len(msg.prs))
		for _, p := range msg.prs {
			items = append(items, reviewItem{p})
		}
		m.reviews.SetItems(items)
		m.updatePreviewForReviews()
		return m, nil

	case reposLoadedMsg:
		m.loading = false
		items := make([]list.Item, 0, len(msg.repos))
		for _, r := range msg.repos {
			items = append(items, repoBrowseItem{r})
		}
		m.browse.SetItems(items)
		if len(items) > 0 {
			m.browse.Select(0)
			if it, ok := items[0].(repoBrowseItem); ok {
				m.loading = true
				return m, tea.Batch(m.spinner.Tick, m.fetchReadme(it.r.Project, it.r.Slug))
			}
		} else {
			m.preview.SetContent(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).
				Render("No repos found in " + m.browseQ))
		}
		return m, nil

	case readmeLoadedMsg:
		m.loading = false
		body := strings.TrimSpace(msg.body)
		if body == "" {
			body = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).
				Render(fmt.Sprintf("(no README found in %s/%s — press p to open PRs)", msg.project, msg.slug))
		}
		header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("159")).
			Render(fmt.Sprintf("📖 %s/%s", msg.project, msg.slug))
		m.preview.SetContent(header + "\n\n" + body)
		m.preview.GotoTop()
		return m, nil

	case homeErrMsg:
		m.loading = false
		m.status = "✗ " + msg.err.Error()
		return m, nil

	case tea.KeyMsg:
		// If the search input is focused, route keys there until the
		// user hits enter or esc.
		if m.search.Focused() {
			switch msg.String() {
			case "enter":
				q := strings.TrimSpace(m.search.Value())
				m.search.Blur()
				if q == "" {
					return m, nil
				}
				m.browseQ = q
				m.loading = true
				return m, tea.Batch(m.spinner.Tick, m.fetchRepos(q))
			case "esc":
				m.search.Blur()
				return m, nil
			}
			var cmd tea.Cmd
			m.search, cmd = m.search.Update(msg)
			return m, cmd
		}

		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Help):
			m.help.ShowAll = !m.help.ShowAll
			m.layout()
			return m, nil
		case key.Matches(msg, m.keys.Tab):
			if m.tab == tabReviews {
				m.tab = tabBrowse
			} else {
				m.tab = tabReviews
			}
			return m, nil
		case key.Matches(msg, m.keys.Search):
			m.tab = tabBrowse
			m.search.Focus()
			return m, textinput.Blink
		case key.Matches(msg, m.keys.OpenPRs):
			project, slug := m.selectedRepoContext()
			if project == "" {
				m.status = "no repo selected"
				return m, nil
			}
			m.next = &HomeAction{Kind: "prs", Project: project, Slug: slug}
			return m, tea.Quit
		case key.Matches(msg, m.keys.Enter):
			switch m.tab {
			case tabReviews:
				if it, ok := m.reviews.SelectedItem().(reviewItem); ok {
					m.next = &HomeAction{Kind: "prs", Project: it.r.Project, Slug: it.r.Slug}
					return m, tea.Quit
				}
			case tabBrowse:
				if it, ok := m.browse.SelectedItem().(repoBrowseItem); ok {
					m.loading = true
					return m, tea.Batch(m.spinner.Tick, m.fetchReadme(it.r.Project, it.r.Slug))
				}
			}
			return m, nil
		case key.Matches(msg, m.keys.Open):
			project, _ := m.selectedRepoContext()
			if project == "" {
				return m, nil
			}
			// Best-effort: construct a probable web URL from the
			// selected item's repo.
			url := ""
			switch it := m.reviews.SelectedItem().(type) {
			case reviewItem:
				url = it.r.PR.WebURL
			default:
				_ = it
			}
			if url == "" {
				if it, ok := m.browse.SelectedItem().(repoBrowseItem); ok {
					url = it.r.WebURL
				}
			}
			if url != "" {
				_ = openInBrowser(url)
			}
			return m, nil
		}

		// Forward unhandled keys to the focused list / preview.
		switch m.tab {
		case tabReviews:
			var cmd tea.Cmd
			prevSel := m.reviews.Index()
			m.reviews, cmd = m.reviews.Update(msg)
			if m.reviews.Index() != prevSel {
				m.updatePreviewForReviews()
			}
			return m, cmd
		case tabBrowse:
			var cmd tea.Cmd
			m.browse, cmd = m.browse.Update(msg)
			return m, cmd
		}
	}

	return m, nil
}

func (m *homeModel) selectedRepoContext() (string, string) {
	switch m.tab {
	case tabReviews:
		if it, ok := m.reviews.SelectedItem().(reviewItem); ok {
			return it.r.Project, it.r.Slug
		}
	case tabBrowse:
		if it, ok := m.browse.SelectedItem().(repoBrowseItem); ok {
			return it.r.Project, it.r.Slug
		}
	}
	return "", ""
}

func (m *homeModel) updatePreviewForReviews() {
	it, ok := m.reviews.SelectedItem().(reviewItem)
	if !ok {
		m.preview.SetContent("")
		return
	}
	p := it.r.PR
	bold := lipgloss.NewStyle().Bold(true)
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	branch := lipgloss.NewStyle().Foreground(lipgloss.Color("159"))
	author := lipgloss.NewStyle().Foreground(lipgloss.Color("213"))

	var sb strings.Builder
	fmt.Fprintf(&sb, "%s  %s\n", bold.Render(fmt.Sprintf("#%d", p.ID)), bold.Render(p.Title))
	fmt.Fprintf(&sb, "%s/%s · %s\n", it.r.Project, it.r.Slug, styleState(p.State))
	fmt.Fprintf(&sb, "%s · %s → %s\n",
		author.Render(p.Author), branch.Render(p.SourceRef), branch.Render(p.TargetRef))
	if !p.UpdatedAt.IsZero() {
		fmt.Fprintln(&sb, muted.Render("updated "+humanTime(p.UpdatedAt)))
	}
	if p.WebURL != "" {
		fmt.Fprintln(&sb, muted.Render(p.WebURL))
	}
	if p.Description != "" {
		fmt.Fprintln(&sb)
		sb.WriteString(p.Description)
	}
	m.preview.SetContent(sb.String())
	m.preview.GotoTop()
}

func (m homeModel) View() string {
	if m.err != nil {
		return statusErr.Render("error: "+m.err.Error()) + "\n\npress q to quit"
	}

	header := titleBar("BB · HOME",
		titleChip.Render(m.svc.Host()),
		titleChipDim.Render("press tab to switch · / to search"),
	)

	tabs := m.renderTabs()

	var leftPane string
	switch m.tab {
	case tabReviews:
		leftPane = m.reviews.View()
	case tabBrowse:
		searchLine := m.search.View()
		if !m.search.Focused() && m.browseQ != "" {
			searchLine = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).
				Render("🔎 " + m.browseQ + "  (press / to search again)")
		}
		leftPane = searchLine + "\n" + m.browse.View()
	}

	leftStyled := lipgloss.NewStyle().Padding(0, 1).Render(leftPane)
	rightStyled := lipgloss.NewStyle().Padding(0, 1).Render(m.preview.View())
	body := lipgloss.JoinHorizontal(lipgloss.Top, leftStyled, rightStyled)

	footer := m.help.View(m.keys)
	statusLine := ""
	if m.loading {
		statusLine = statusInfo.Render(m.spinner.View() + " loading…")
	} else if m.status != "" {
		switch {
		case strings.HasPrefix(m.status, "✗"):
			statusLine = statusErr.Render(m.status)
		case strings.HasPrefix(m.status, "✓"):
			statusLine = statusOK.Render(m.status)
		default:
			statusLine = statusInfo.Render(m.status)
		}
	}
	if statusLine != "" {
		footer = statusLine + "  " + titleSep + "  " + footer
	}

	return header + "\n" + tabs + "\n" + body + "\n" + footer
}

func (m homeModel) renderTabs() string {
	active := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).
		Background(lipgloss.Color("57")).Padding(0, 2)
	inactive := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Padding(0, 2)
	tabs := []string{"Reviews", "Browse"}
	out := make([]string, len(tabs))
	for i, t := range tabs {
		if int(m.tab) == i {
			out[i] = active.Render(t)
		} else {
			out[i] = inactive.Render(t)
		}
	}
	return strings.Join(out, " ")
}
