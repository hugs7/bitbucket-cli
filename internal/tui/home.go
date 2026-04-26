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
	"time"

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

// HomeState captures the parts of the home model that should survive
// across a "launch PRs and come back" round-trip, so the user lands
// back exactly where they were instead of on the first tab.
type HomeState struct {
	Tab       homeTab
	BrowseQ   string
	DashIdx   int
	FavIdx    int
	BrowseIdx int
}

// Home launches the dashboard. The optional prev state restores the
// last tab, search and cursor positions when the user has been
// bounced through a sub-TUI (PRs) and returned.
func Home(svc api.Service, prev *HomeState) (*HomeAction, *HomeState, error) {
	m := newHomeModel(svc)
	if prev != nil {
		m.tab = prev.Tab
		m.browseQ = prev.BrowseQ
		m.restore = prev
		// If a previous search will be auto-replayed by Init, flag
		// the searching state up front so the very first frame shows
		// the loader instead of an empty list.
		if prev.BrowseQ != "" {
			m.searching = true
			m.loading = true
		}
	}
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	final, err := p.Run()
	if err != nil {
		return nil, nil, err
	}
	hm := final.(homeModel)
	state := &HomeState{
		Tab:       hm.tab,
		BrowseQ:   hm.browseQ,
		DashIdx:   hm.dashCursor,
		FavIdx:    hm.favs.Index(),
		BrowseIdx: hm.browse.Index(),
	}
	return hm.next, state, nil
}

type homeTab int

// tabDashboard hosts the multi-section dashboard view (review queue,
// your authored PRs, recently closed PRs, recently viewed repos).
// The other two tabs are simple list-of-repo views.
const (
	tabDashboard homeTab = iota
	tabFavourites
	tabBrowse
)

var allTabs = []homeTab{tabDashboard, tabFavourites, tabBrowse}

func (t homeTab) name() string {
	switch t {
	case tabDashboard:
		return "Dashboard"
	case tabFavourites:
		return "★ Favourites"
	case tabBrowse:
		return "Browse"
	}
	return "?"
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

	favs    list.Model
	browse  list.Model
	search  textinput.Model
	preview viewport.Model
	spinner spinner.Model

	// Dashboard sections — each loaded in parallel from Init.
	// dashCursor walks the flattened, selectable rows across all
	// sections (section headers are skipped).
	reviewPRs    []api.ReviewPR
	authoredPRs  []api.ReviewPR
	closedPRs    []api.ReviewPR
	recentRepos  []api.Repo
	dashCursor  int
	prevDashRow string // previous selected row identity, for preview-refresh detection

	loading   bool
	searching bool // true while a SearchRepos call is in flight
	status    string
	err       error
	browseQ   string // last search executed

	// Hot-search debounce: each keystroke increments searchVersion
	// and schedules a Tick. The Tick handler only fires the actual
	// SearchRepos call if its captured version is still current,
	// effectively cancelling stale in-flight requests for free.
	searchVersion int

	// searchCache memoises results per query for the lifetime of
	// this home session so re-typing or re-searching the same query
	// is instant and doesn't re-hit the API.
	searchCache map[string][]api.Repo

	// restore carries cursor positions / search to re-apply after the
	// underlying lists have been populated (Init / load callbacks).
	// Cleared after each field is consumed.
	restore *HomeState

	next *HomeAction
}

type reviewsLoadedMsg struct{ prs []api.ReviewPR }
type authoredLoadedMsg struct{ prs []api.ReviewPR }
type closedLoadedMsg struct{ prs []api.ReviewPR }
type recentReposLoadedMsg struct{ repos []api.Repo }
type reposLoadedMsg struct{ repos []api.Repo }
type readmeLoadedMsg struct {
	project, slug string
	body          string
}
type searchTickMsg struct {
	q       string
	version int
}
type homeErrMsg struct{ err error }

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
func accentDelegate() list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	d.Styles.SelectedTitle = d.Styles.SelectedTitle.
		BorderForeground(lipgloss.Color("57")).
		Foreground(lipgloss.Color("231")).
		Bold(true)
	d.Styles.SelectedDesc = d.Styles.SelectedDesc.
		BorderForeground(lipgloss.Color("57")).
		Foreground(lipgloss.Color("159"))
	return d
}

func newHomeModel(svc api.Service) homeModel {
	fl := list.New(nil, accentDelegate(), 0, 0)
	fl.Title = "Pinned repos"
	fl.SetShowHelp(false)

	bl := list.New(nil, accentDelegate(), 0, 0)
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
		svc:         svc,
		tab:         tabDashboard,
		keys:        defaultHomeKeys(),
		help:        help.New(),
		favs:        fl,
		browse:      bl,
		search:      ti,
		preview:     pv,
		spinner:     sp,
		loading:     true,
		searchCache: map[string][]api.Repo{},
	}
	m.refreshFavourites()
	return m
}

// applyRestore re-seats list cursors after data has loaded so a
// returning user lands back where they were.
func (m *homeModel) applyRestore() {
	if m.restore == nil {
		return
	}
	rows := m.dashFlatRows()
	if m.restore.DashIdx >= 0 && m.restore.DashIdx < len(rows) {
		m.dashCursor = m.restore.DashIdx
	}
	if m.restore.FavIdx >= 0 && m.restore.FavIdx < len(m.favs.Items()) {
		m.favs.Select(m.restore.FavIdx)
	}
	if m.restore.BrowseIdx >= 0 && m.restore.BrowseIdx < len(m.browse.Items()) {
		m.browse.Select(m.restore.BrowseIdx)
	}
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
	// Fan out all four dashboard sections in parallel so the page
	// fills in piecewise as each endpoint replies — no single slow
	// section blocks the rest.
	cmds := []tea.Cmd{
		m.spinner.Tick,
		m.fetchReviews(),
		m.fetchAuthored(),
		m.fetchClosed(),
		m.fetchRecentRepos(),
	}
	if m.restore != nil && m.restore.FavIdx >= 0 && m.restore.FavIdx < len(m.favs.Items()) {
		m.favs.Select(m.restore.FavIdx)
	}
	if m.browseQ != "" {
		m.searching = true
		m.loading = true
		cmds = append(cmds, m.fetchRepos(m.browseQ))
	}
	return tea.Batch(cmds...)
}

func (m homeModel) fetchReviews() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		prs, err := svc.ListMyReviewPRs(10)
		if err != nil {
			return homeErrMsg{err: err}
		}
		return reviewsLoadedMsg{prs: prs}
	}
}

func (m homeModel) fetchAuthored() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		prs, err := svc.ListMyAuthoredPRs(10)
		if err != nil {
			return homeErrMsg{err: err}
		}
		return authoredLoadedMsg{prs: prs}
	}
}

func (m homeModel) fetchClosed() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		prs, err := svc.ListRecentlyClosedPRs(10)
		if err != nil {
			return homeErrMsg{err: err}
		}
		return closedLoadedMsg{prs: prs}
	}
}

func (m homeModel) fetchRecentRepos() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		repos, err := svc.ListRecentlyViewedRepos(10)
		if err != nil {
			return homeErrMsg{err: err}
		}
		return recentReposLoadedMsg{repos: repos}
	}
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

func (m *homeModel) layout() {
	helpH := lipgloss.Height(m.help.View(m.keys))
	// header(1) + tabs(1) + tab-underline(1) + footer(helpH) + a
	// blank line between body and footer = helpH + 4.
	contentH := m.height - helpH - 4
	if contentH < 5 {
		contentH = 5
	}

	// Reserve a 1-cell column for the vertical separator between
	// the two bordered panes.
	usable := m.width - 1
	leftW := usable / 2
	if leftW < 32 {
		leftW = usable
	}
	rightW := usable - leftW

	// Each pane is wrapped in a rounded border (adds 2 cells on
	// every side), so the inner content area is paneW-2 / paneH-2.
	listInnerW := leftW - 2
	listInnerH := contentH - 2
	browseListInnerH := listInnerH - 3 // search box (border + 1 line)

	m.favs.SetSize(listInnerW, listInnerH)
	m.browse.SetSize(listInnerW, browseListInnerH)
	m.search.Width = listInnerW - 4
	m.preview.Width = rightW - 2
	m.preview.Height = listInnerH
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
		m.reviewPRs = msg.prs
		return m, m.afterDashLoad()

	case authoredLoadedMsg:
		m.loading = false
		m.authoredPRs = msg.prs
		return m, m.afterDashLoad()

	case closedLoadedMsg:
		m.loading = false
		m.closedPRs = msg.prs
		return m, m.afterDashLoad()

	case recentReposLoadedMsg:
		m.loading = false
		m.recentRepos = msg.repos
		return m, m.afterDashLoad()

	case reposLoadedMsg:
		m.loading = false
		m.searching = false
		// Cache by the query that produced these results so the
		// next time the user types it (or hits Enter on it) we
		// don't refetch.
		m.searchCache[m.browseQ] = msg.repos
		items := make([]list.Item, 0, len(msg.repos))
		for _, r := range msg.repos {
			items = append(items, repoBrowseItem{r})
		}
		m.browse.SetItems(items)
		if len(items) == 0 {
			m.preview.SetContent(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).
				Render("No repos found in " + m.browseQ))
			return m, nil
		}
		idx := 0
		if m.restore != nil && m.restore.BrowseIdx >= 0 && m.restore.BrowseIdx < len(items) {
			idx = m.restore.BrowseIdx
			m.restore = nil // consume — only restore once per round-trip
		}
		m.browse.Select(idx)
		if it, ok := items[idx].(repoBrowseItem); ok {
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, m.fetchReadme(it.r.Project, it.r.Slug))
		}
		return m, nil

	case readmeLoadedMsg:
		m.loading = false
		body := strings.TrimSpace(msg.body)
		if body == "" {
			body = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).
				Render("(no README found — press p to open this repo's PRs, o to open in browser)")
		}
		m.preview.SetContent(m.readmeHeader(msg.project, msg.slug) + "\n\n" + body)
		m.preview.GotoTop()
		return m, nil

	case searchTickMsg:
		// Stale tick (a newer keystroke superseded this one) — drop.
		if msg.version != m.searchVersion {
			return m, nil
		}
		// Empty query: clear results, no API call.
		if msg.q == "" {
			m.browse.SetItems(nil)
			m.browseQ = ""
			m.loading = false
			m.searching = false
			return m, nil
		}
		// Already searching for or showing this query — skip.
		if msg.q == m.browseQ {
			return m, nil
		}
		m.browseQ = msg.q
		// Cache hit — no API round-trip needed.
		if cached, ok := m.searchCache[msg.q]; ok {
			return m, m.applyCachedResults(cached)
		}
		m.loading = true
		m.searching = true
		return m, tea.Batch(m.spinner.Tick, m.fetchRepos(msg.q))

	case homeErrMsg:
		m.loading = false
		m.searching = false
		m.status = "✗ " + msg.err.Error()
		return m, nil

	case tea.KeyMsg:
		// If the search input is focused, route most keys there until
		// the user hits enter or esc. Arrow keys / PgUp / PgDn /
		// Home / End fall through to the browse list so users can
		// navigate results without having to blur the input first.
		// Enter on the search input drills into the highlighted
		// repo if results are already loaded (no re-search). Ctrl-U
		// clears the buffer in place so users can re-search without
		// manually backspacing. Every other keystroke schedules a
		// debounced "hot search" so results update as the user types.
		if m.search.Focused() {
			switch msg.String() {
			case "enter":
				q := strings.TrimSpace(m.search.Value())
				// If we have results and the field still matches
				// the last executed query, treat Enter as
				// "open the selected repo" — never re-search.
				if q == m.browseQ && len(m.browse.Items()) > 0 {
					m.search.Blur()
					if it, ok := m.browse.SelectedItem().(repoBrowseItem); ok {
						m.next = &HomeAction{Kind: "prs", Project: it.r.Project, Slug: it.r.Slug}
						return m, tea.Quit
					}
					return m, nil
				}
				m.search.Blur()
				if q == "" {
					return m, nil
				}
				return m, m.runSearchNow(q)
			case "esc":
				m.search.Blur()
				return m, nil
			case "ctrl+u":
				m.search.SetValue("")
				m.browse.SetItems(nil)
				m.browseQ = ""
				return m, nil
			case "up", "down", "pgup", "pgdown", "home", "end":
				// Navigate the results list while keeping the
				// input focused so the user can continue typing.
				var cmd tea.Cmd
				prev := m.browse.Index()
				m.browse, cmd = m.browse.Update(msg)
				if m.browse.Index() != prev {
					if it, ok := m.browse.SelectedItem().(repoBrowseItem); ok {
						m.loading = true
						return m, tea.Batch(cmd, m.spinner.Tick, m.fetchReadme(it.r.Project, it.r.Slug))
					}
				}
				return m, cmd
			}
			var cmd tea.Cmd
			m.search, cmd = m.search.Update(msg)
			return m, tea.Batch(cmd, m.scheduleHotSearch())
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
			// Enter is the consistent "open this repo" action across
			// every tab — drills into the PR TUI for the selected
			// repo. README loading happens automatically on j/k
			// navigation, so Enter doesn't need to (re)trigger it.
			switch m.tab {
			case tabDashboard:
				if r := m.selectedDashRow(); r != nil {
					m.next = &HomeAction{Kind: "prs", Project: r.project(), Slug: r.slug()}
					return m, tea.Quit
				}
			case tabFavourites:
				if it, ok := m.favs.SelectedItem().(repoBrowseItem); ok {
					m.next = &HomeAction{Kind: "prs", Project: it.r.Project, Slug: it.r.Slug}
					return m, tea.Quit
				}
			case tabBrowse:
				if it, ok := m.browse.SelectedItem().(repoBrowseItem); ok {
					m.next = &HomeAction{Kind: "prs", Project: it.r.Project, Slug: it.r.Slug}
					return m, tea.Quit
				}
			}
			return m, nil
		case key.Matches(msg, m.keys.Open):
			// Best-effort: derive a web URL from whichever pane
			// has the selection.
			url := ""
			switch m.tab {
			case tabDashboard:
				if r := m.selectedDashRow(); r != nil {
					if r.kind == "pr" {
						url = r.pr.PR.WebURL
					} else {
						url = r.repo.WebURL
					}
				}
			case tabFavourites:
				if it, ok := m.favs.SelectedItem().(repoBrowseItem); ok {
					url = it.r.WebURL
				}
			case tabBrowse:
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
		case tabDashboard:
			return m, m.dashboardKey(msg)
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
	case tabDashboard:
		if r := m.selectedDashRow(); r != nil {
			return r.project(), r.slug()
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
	case tabDashboard:
		if r := m.selectedDashRow(); r != nil {
			if r.kind == "repo" && r.repo.Name != "" {
				return r.repo.Name
			}
			return r.slug()
		}
	}
	return ""
}

// refreshPreviewForTab returns a Cmd that updates the right pane to
// reflect the currently-selected item on the newly-active tab.
func (m *homeModel) refreshPreviewForTab() tea.Cmd {
	switch m.tab {
	case tabDashboard:
		return m.refreshDashboardPreview(true)
	case tabFavourites:
		if it, ok := m.favs.SelectedItem().(repoBrowseItem); ok {
			m.loading = true
			return tea.Batch(m.spinner.Tick, m.fetchReadme(it.r.Project, it.r.Slug))
		}
		m.preview.SetContent(homeMuted.
			Render("Pin a repo with f from the Dashboard or Browse tab."))
	case tabBrowse:
		// Leave browse preview as the most recent README.
	}
	return nil
}

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
	return m.refreshDashboardPreview(false)
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
	fmt.Fprintf(&sb, "%s/%s · %s\n", rp.Project, rp.Slug, styleState(p.State))
	fmt.Fprintf(&sb, "%s · %s → %s\n",
		author.Render(p.Author), branch.Render(p.SourceRef), branch.Render(p.TargetRef))
	if !p.UpdatedAt.IsZero() {
		fmt.Fprintln(&sb, homeMuted.Render("updated "+humanTime(p.UpdatedAt)))
	}
	if p.WebURL != "" {
		fmt.Fprintln(&sb, homeMuted.Render(p.WebURL))
	}
	if p.Description != "" {
		fmt.Fprintln(&sb)
		sb.WriteString(p.Description)
	}
	return sb.String()
}

// homeMuted is the canonical muted-hint colour used across home.
var homeMuted = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

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
		Border(lipgloss.RoundedBorder()).
		BorderForeground(c).
		Width(w - 2).
		Height(h - 2)
	return s
}

// card renders a small bordered card (used for empty-state and loader
// messages) sized to fit the inner pane area.
func card(borderColor string, w, h int, body string) string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
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
	title := titleBadge.Render(fmt.Sprintf(" %s/%s ", project, slug))
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
		chips = append(chips, titleSep, titleChip.Render("⎇ "+repo.DefaultRef))
	}
	if repo.Description != "" {
		chips = append(chips, titleSep, titleChipDim.Render(repo.Description))
	}
	header := strings.Join(chips, "")
	hint := homeMuted.Render("p → PRs  ·  o → browser  ·  f → favourite")
	if repo.WebURL != "" {
		hint = homeMuted.Render(repo.WebURL) + "\n" + hint
	}
	return header + "\n" + hint
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

func (m homeModel) View() string {
	if m.err != nil {
		return statusErr.Render("error: "+m.err.Error()) + "\n\npress q to quit"
	}
	if m.width == 0 {
		return "" // wait for first WindowSizeMsg before painting.
	}

	header := titleBar("BB · HOME",
		titleChip.Render(m.svc.Host()),
		titleChipDim.Render("tab to switch · / to search · ? for help"),
	)

	helpH := lipgloss.Height(m.help.View(m.keys))
	contentH := m.height - helpH - 4
	if contentH < 5 {
		contentH = 5
	}
	usable := m.width - 1
	leftW := usable / 2
	if leftW < 32 {
		leftW = usable
	}
	rightW := usable - leftW
	listInnerW := leftW - 2
	listInnerH := contentH - 2
	browseListInnerH := listInnerH - 3

	tabs := m.renderTabs()

	// Compose the inner content of the left pane based on the active tab.
	var leftInner string
	switch m.tab {
	case tabDashboard:
		leftInner = m.renderDashboard(listInnerW, listInnerH)
	case tabFavourites:
		if len(m.favs.Items()) == 0 {
			leftInner = card("245", listInnerW, listInnerH,
				homeMuted.Render("No favourites yet.\n\nPress ")+
					titleChip.Render("f")+
					homeMuted.Render(" on a repo in Dashboard or Browse to pin it."))
		} else {
			leftInner = m.favs.View()
		}
	case tabBrowse:
		var listView string
		switch {
		case m.searching:
			body := homeLoadBanner.Render(m.spinner.View()+" searching for "+m.browseQ+"…") +
				"\n\n" + homeMuted.Render("(scanning the full repo list — this may take a moment)")
			listView = card("57", listInnerW, browseListInnerH, body)
		case m.browseQ != "" && len(m.browse.Items()) == 0:
			listView = card("245", listInnerW, browseListInnerH,
				homeMuted.Render("No repos matched ")+titleChipWarn.Render(m.browseQ)+homeMuted.Render("."))
		case m.browseQ == "" && len(m.browse.Items()) == 0:
			listView = card("245", listInnerW, browseListInnerH,
				homeMuted.Render("Type to search across all repos.\n\nResults stream in as you type."))
		default:
			listView = m.browse.View()
		}
		leftInner = m.searchBox(listInnerW) + "\n" + listView
	}

	leftPane := paneBorder(true, leftW, contentH).Render(leftInner)
	rightPane := paneBorder(false, rightW, contentH).Render(m.preview.View())

	// Subtle vertical separator between panes.
	sepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	sepCol := sepStyle.Render(strings.TrimRight(strings.Repeat("│\n", contentH), "\n"))

	body := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, sepCol, rightPane)

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

// renderTabs draws the tab strip with a clear active-tab pill and a
// matching underline beneath it so the eye can immediately locate the
// current tab without re-reading colours.
func (m homeModel) renderTabs() string {
	active := lipgloss.NewStyle().Bold(true).
		Foreground(lipgloss.Color("231")).
		Background(lipgloss.Color("57")).
		Padding(0, 2)
	inactive := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245")).
		Padding(0, 2)
	underlineStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("57"))

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
			ub.WriteString(underlineStyle.Render(strings.Repeat("▔", w)))
		} else {
			ub.WriteString(strings.Repeat(" ", w))
		}
	}
	return row + "\n" + ub.String()
}

// dashboard rendering ---------------------------------------------

var (
	dashSecHeader = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("231")).Background(lipgloss.Color("57")).
			Padding(0, 1)
	dashSecCount = lipgloss.NewStyle().Foreground(lipgloss.Color("159"))
	dashRowSel   = lipgloss.NewStyle().
			Foreground(lipgloss.Color("231")).Bold(true).
			Border(lipgloss.Border{Left: "▎"}, false, false, false, true).
			BorderForeground(lipgloss.Color("213")).
			PaddingLeft(1)
	dashRowIdle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).
			PaddingLeft(2)
	dashRowMeta = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

// renderDashboard renders the four stacked sections inside the left
// pane. Long dashboards scroll naturally because we only emit the
// first listInnerH lines (rest is clipped by the bordered pane).
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

	// Walk sections, rendering header + up to N rows. Track the
	// global row index so we can highlight the row under dashCursor.
	var sb strings.Builder
	globalIdx := 0
	for i, sec := range sections {
		if i > 0 {
			sb.WriteString("\n")
		}
		count := len(sec.rows)
		header := dashSecHeader.Render(" "+sec.title+" ") + " " +
			dashSecCount.Render(fmt.Sprintf("(%d)", count))
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
	return sb.String()
}

// renderDashRow renders a single dashboard row (PR or repo). The
// selected row gets a left accent bar; idle rows are dim with subtle
// metadata.
func (m *homeModel) renderDashRow(r dashRow, selected bool, innerW int) string {
	var title, meta string
	if r.kind == "pr" {
		p := r.pr.PR
		title = fmt.Sprintf("#%d  %s", p.ID, truncate(p.Title, innerW-12))
		meta = fmt.Sprintf("%s/%s · %s · %s",
			r.pr.Project, r.pr.Slug, p.Author, humanTime(p.UpdatedAt))
	} else {
		title = fmt.Sprintf("%s/%s", r.repo.Project, r.repo.Slug)
		desc := r.repo.Description
		if desc == "" {
			desc = "(no description)"
		}
		meta = truncate(desc, innerW-6)
	}
	if selected {
		return dashRowSel.Render(title) + "\n" +
			dashRowMeta.PaddingLeft(3).Render(meta)
	}
	return dashRowIdle.Render(title) + "\n" +
		dashRowMeta.PaddingLeft(3).Render(meta)
}

func truncate(s string, max int) string {
	if max < 4 || len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
