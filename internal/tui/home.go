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
	"github.com/hugs7/bitbucket-cli/internal/config"
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
	tabFavourites
	tabBrowse
)

var allTabs = []homeTab{tabReviews, tabFavourites, tabBrowse}

func (t homeTab) name() string {
	switch t {
	case tabReviews:
		return "Reviews"
	case tabFavourites:
		return "★ Favourites"
	case tabBrowse:
		return "Browse"
	}
	return "?"
}

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
	Up, Down, Enter, Tab, ShiftTab, Search, Open, Quit, Back, Help, OpenPRs, ToggleFav key.Binding
}

func defaultHomeKeys() homeKeys {
	return homeKeys{
		Up:        key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:      key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Enter:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open / load")),
		Tab:       key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next tab")),
		ShiftTab:  key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("⇧tab", "prev tab")),
		Search:    key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		Open:      key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "browser")),
		OpenPRs:   key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "open PRs")),
		ToggleFav: key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "favourite")),
		Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Back:      key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back / blur")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	}
}

func (k homeKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Enter, k.Tab, k.Search, k.OpenPRs, k.ToggleFav, k.Open, k.Help, k.Quit}
}
func (k homeKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Enter, k.Tab, k.ShiftTab, k.Search},
		{k.OpenPRs, k.ToggleFav, k.Open, k.Help, k.Back, k.Quit},
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
	favs     list.Model
	browse   list.Model
	search   textinput.Model
	preview  viewport.Model
	spinner  spinner.Model
	loading  bool
	status   string
	err      error
	browseQ  string // last search executed

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

	fl := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	fl.Title = "Pinned repos"
	fl.SetShowHelp(false)

	bl := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	bl.Title = "Search results"
	bl.SetShowHelp(false)

	ti := textinput.New()
	ti.Placeholder = "name fragment, e.g. checkout · or workspace/name"
	ti.Prompt = "🔎 "
	ti.CharLimit = 120

	pv := viewport.New(0, 0)

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	m := homeModel{
		svc:     svc,
		tab:     tabReviews,
		keys:    defaultHomeKeys(),
		help:    help.New(),
		reviews: rl,
		favs:    fl,
		browse:  bl,
		search:  ti,
		preview: pv,
		spinner: sp,
		loading: true,
	}
	m.refreshFavourites()
	return m
}

// refreshFavourites pulls the persisted favourites list (filtered to
// the current host) into the favs list model.
func (m *homeModel) refreshFavourites() {
	host := m.svc.Host()
	items := []list.Item{}
	for _, f := range config.Get().Favourites {
		if f.Host != host {
			continue
		}
		name := f.Name
		if name == "" {
			name = f.Slug
		}
		items = append(items, repoBrowseItem{r: api.Repo{
			Project: f.Project, Slug: f.Slug, Name: name,
		}})
	}
	m.favs.SetItems(items)
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

func (m *homeModel) layout() {
	helpH := lipgloss.Height(m.help.View(m.keys))
	contentH := m.height - helpH - 3 // header + tabs + footer

	leftW := m.width / 2
	if leftW < 30 {
		leftW = m.width
	}
	rightW := m.width - leftW - 2

	m.reviews.SetSize(leftW, contentH)
	m.favs.SetSize(leftW, contentH)
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
			body = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).
				Render("(no README found — press p to open this repo's PRs, o to open in browser)")
		}
		header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("159")).
			Render(fmt.Sprintf("📖 %s/%s", msg.project, msg.slug))
		hint := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).
			Render("press p → PRs · o → browser · f → favourite")
		m.preview.SetContent(header + "\n" + hint + "\n\n" + body)
		m.preview.GotoTop()
		return m, nil

	case homeErrMsg:
		m.loading = false
		m.status = "✗ " + msg.err.Error()
		return m, nil

	case tea.KeyMsg:
		// If the search input is focused, route keys there until the
		// user hits enter or esc. Ctrl-U clears the buffer in place
		// (vim-style) so users can re-search without manually
		// backspacing the previous query.
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
			case "ctrl+u":
				m.search.SetValue("")
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
			m.tab = (m.tab + 1) % homeTab(len(allTabs))
			return m, m.refreshPreviewForTab()
		case key.Matches(msg, m.keys.ShiftTab):
			m.tab = (m.tab + homeTab(len(allTabs)-1)) % homeTab(len(allTabs))
			return m, m.refreshPreviewForTab()
		case key.Matches(msg, m.keys.ToggleFav):
			project, slug := m.selectedRepoContext()
			name := m.selectedRepoName()
			if project == "" {
				m.status = "no repo selected"
				return m, nil
			}
			host := m.svc.Host()
			if config.IsFavourite(host, project, slug) {
				_ = config.RemoveFavourite(host, project, slug)
				m.status = fmt.Sprintf("✓ removed %s/%s from favourites", project, slug)
			} else {
				_ = config.AddFavourite(config.FavRepo{Host: host, Project: project, Slug: slug, Name: name})
				m.status = fmt.Sprintf("✓ added %s/%s to favourites", project, slug)
			}
			m.refreshFavourites()
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
			case tabFavourites:
				if it, ok := m.favs.SelectedItem().(repoBrowseItem); ok {
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
		case tabFavourites:
			var cmd tea.Cmd
			prevSel := m.favs.Index()
			m.favs, cmd = m.favs.Update(msg)
			if m.favs.Index() != prevSel {
				if it, ok := m.favs.SelectedItem().(repoBrowseItem); ok {
					m.loading = true
					return m, tea.Batch(cmd, m.spinner.Tick, m.fetchReadme(it.r.Project, it.r.Slug))
				}
			}
			return m, cmd
		case tabBrowse:
			var cmd tea.Cmd
			prevSel := m.browse.Index()
			m.browse, cmd = m.browse.Update(msg)
			// Live README preview: any time the highlighted repo
			// changes (j/k, arrow keys, page, …), kick off a fetch
			// so the right pane stays in sync without a separate
			// "open" step.
			if m.browse.Index() != prevSel {
				if it, ok := m.browse.SelectedItem().(repoBrowseItem); ok {
					m.loading = true
					return m, tea.Batch(cmd, m.spinner.Tick, m.fetchReadme(it.r.Project, it.r.Slug))
				}
			}
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
	case tabFavourites:
		if it, ok := m.favs.SelectedItem().(repoBrowseItem); ok {
			return it.r.Project, it.r.Slug
		}
	case tabBrowse:
		if it, ok := m.browse.SelectedItem().(repoBrowseItem); ok {
			return it.r.Project, it.r.Slug
		}
	}
	return "", ""
}

func (m *homeModel) selectedRepoName() string {
	switch m.tab {
	case tabFavourites:
		if it, ok := m.favs.SelectedItem().(repoBrowseItem); ok {
			return it.r.Name
		}
	case tabBrowse:
		if it, ok := m.browse.SelectedItem().(repoBrowseItem); ok {
			return it.r.Name
		}
	case tabReviews:
		if it, ok := m.reviews.SelectedItem().(reviewItem); ok {
			return it.r.Slug
		}
	}
	return ""
}

// refreshPreviewForTab returns a Cmd that updates the right pane to
// reflect the currently-selected item on the newly-active tab.
func (m *homeModel) refreshPreviewForTab() tea.Cmd {
	switch m.tab {
	case tabReviews:
		m.updatePreviewForReviews()
	case tabFavourites:
		if it, ok := m.favs.SelectedItem().(repoBrowseItem); ok {
			m.loading = true
			return tea.Batch(m.spinner.Tick, m.fetchReadme(it.r.Project, it.r.Slug))
		}
		m.preview.SetContent(lipgloss.NewStyle().Foreground(lipgloss.Color("245")).
			Render("Pin a repo with f from the Reviews or Browse tab."))
	case tabBrowse:
		// Leave browse preview as the most recent README.
	}
	return nil
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
	case tabFavourites:
		if len(m.favs.Items()) == 0 {
			leftPane = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).
				Render("No favourites yet — press f on a repo (in Reviews or Browse) to pin it.")
		} else {
			leftPane = m.favs.View()
		}
	case tabBrowse:
		var searchLine string
		if m.search.Focused() {
			searchLine = m.search.View()
		} else if m.browseQ != "" {
			searchLine = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).
				Render("🔎 " + m.browseQ + "   (/ to search again · ctrl-u to clear in field)")
		} else {
			searchLine = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).
				Render("Press / to search.")
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
	out := make([]string, 0, len(allTabs))
	for _, t := range allTabs {
		label := t.name()
		if t == tabFavourites {
			n := len(m.favs.Items())
			if n > 0 {
				label = fmt.Sprintf("%s (%d)", label, n)
			}
		}
		if t == m.tab {
			out = append(out, active.Render(label))
		} else {
			out = append(out, inactive.Render(label))
		}
	}
	return strings.Join(out, " ")
}
