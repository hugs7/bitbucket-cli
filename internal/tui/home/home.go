// Package tui — home page model.
//
// The home model is the entry point you get from running `bb` with no
// args: a two-pane dashboard with tabs for "Reviews" (PRs assigned to
// you across hosts) and "Browse" (search a project / workspace and
// drill into its repos to see the README, then jump into the PR TUI).
package home

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
	"github.com/hugs7/bitbucket-cli/internal/sysutil"
	"github.com/hugs7/bitbucket-cli/internal/tui/preview"
	"github.com/hugs7/bitbucket-cli/internal/tui/settings"
	"github.com/hugs7/bitbucket-cli/internal/tui/theme"
)

// Action is what Home returns to its caller when the user picks an
// action that requires launching a different TUI program (e.g. "open
// the PR browser for this repo"). nil means "user quit, we're done".
type Action struct {
	Kind    string // "prs"
	Project string
	Slug    string
}

// State captures the parts of the home model that should survive
// across a "launch PRs and come back" round-trip, so the user lands
// back exactly where they were instead of on the first tab.
type State struct {
	Tab       homeTab
	BrowseQ   string
	DashIdx   int
	FavIdx    int
	BrowseIdx int
}

// Home launches the dashboard. The optional prev state restores the
// last tab, search and cursor positions when the user has been
// bounced through a sub-TUI (PRs) and returned.
func Run(svc api.Service, prev *State) (*Action, *State, error) {
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
	state := &State{
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
		if theme.Mainframe() {
			return "* Favourites"
		}
		return "★ Favourites"
	case tabBrowse:
		return "Browse"
	}
	return "?"
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
	reviewPRs   []api.ReviewPR
	authoredPRs []api.ReviewPR
	closedPRs   []api.ReviewPR
	recentRepos []api.Repo
	dashCursor  int
	dashVP      viewport.Model // scrolls when content exceeds the left pane height
	prevDashRow string         // previous selected row identity, for preview-refresh detection

	// dashFocus picks which pane on the dashboard tab consumes
	// scroll / arrow-key input: "left" routes to the row list (the
	// classic flow — j/k moves the cursor and snaps the dashVP),
	// "right" routes to the README/PR-detail preview viewport so
	// long markdown can be scrolled. Toggled with the "T" key.
	dashFocus string

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
	restore *State

	// settings overlay (toggled with `,`).
	settings     settings.Model
	settingsOpen bool

	next *Action
}


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
	// Apply the user's persisted theme up-front so every style we
	// build below (and every chrome helper Border/SearchPrompt call)
	// already reflects the active palette.
	theme.Init()

	fl := list.New(nil, accentDelegate(), 0, 0)
	fl.Title = "Pinned repos"
	fl.SetShowHelp(false)

	bl := list.New(nil, accentDelegate(), 0, 0)
	bl.Title = "Search results"
	bl.SetShowHelp(false)

	ti := textinput.New()
	ti.Placeholder = "name fragment, e.g. checkout · or workspace/name"
	ti.Prompt = theme.SearchPrompt()
	ti.CharLimit = 120

	pv := viewport.New(0, 0)
	dashVP := viewport.New(0, 0)

	sp := spinner.New()
	// Use the simple |/-\ TTY spinner under the 3270 theme so it
	// matches the operator-area "X SYSTEM" cadence; everywhere else
	// the soft braille dot reads better on modern terminals.
	if theme.Mainframe() {
		sp.Spinner = spinner.Line
	} else {
		sp.Spinner = spinner.Dot
	}

	m := homeModel{
		svc:         svc,
		tab:         tabDashboard,
		dashVP:      dashVP,
		keys:        defaultHomeKeys(),
		help:        help.New(),
		favs:        fl,
		browse:      bl,
		search:      ti,
		preview:     pv,
		spinner:     sp,
		settings:    settings.New(),
		loading:     true,
		searchCache: map[string][]api.Repo{},
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
	m.dashVP.Width = listInnerW
	m.dashVP.Height = listInnerH
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
		// Description is empty here because home already surfaces it
		// in the readmeHeader chip strip — adding it again would
		// just duplicate the metadata above the README body.
		body := preview.Body(msg.body, "", m.preview.Width,
			"(no README found — press p to open this repo's PRs, o to open in browser)")
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
		m.status = theme.ErrPrefix() + msg.err.Error()
		return m, nil

	case tea.MouseMsg:
		// Wheel events (and any future click handling) flow to the
		// component that owns the active tab's body. The viewport
		// and list components handle MouseWheelUp/Down natively, so
		// scroll just works once the message reaches them. Without
		// this dispatcher mouse-wheel input was silently dropped on
		// the dashboard tab — only j/k keyboard navigation moved
		// the view.
		switch m.tab {
		case tabDashboard:
			// When the user has focused the right (preview) pane
			// with T, route wheel events to the README viewport so
			// long markdown can be scrolled without leaving the
			// dashboard tab.
			if m.dashFocus == "right" {
				var cmd tea.Cmd
				m.preview, cmd = m.preview.Update(msg)
				return m, cmd
			}
			// dashVP's line buffer is populated by renderDashboard
			// during View(), but View runs on a throwaway copy of
			// the model — so the stored model's dashVP.lines is
			// empty when this MouseMsg arrives, and viewport's
			// ScrollDown returns immediately ("len(lines)==0"
			// guard). Refresh content here so the wheel actually
			// scrolls.
			innerW := m.dashVP.Width
			if innerW == 0 {
				innerW = m.width
			}
			m.refreshDashContent(innerW)
			var cmd tea.Cmd
			m.dashVP, cmd = m.dashVP.Update(msg)
			return m, cmd
		case tabFavourites:
			var cmd tea.Cmd
			m.favs, cmd = m.favs.Update(msg)
			return m, cmd
		case tabBrowse:
			var cmd tea.Cmd
			m.browse, cmd = m.browse.Update(msg)
			return m, cmd
		}
		return m, nil

	case tea.KeyMsg:
		// Settings overlay owns all keys while open: esc closes,
		// q / ctrl+c still quit, everything else flows into the
		// settings list (navigation + enter/space toggles).
		if m.settingsOpen {
			switch {
			case key.Matches(msg, m.keys.Quit):
				return m, tea.Quit
			case key.Matches(msg, m.keys.Back):
				m.settingsOpen = false
				return m, nil
			}
			var cmd tea.Cmd
			m.settings, cmd = m.settings.Update(msg)
			return m, cmd
		}

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
						// Same destination as "Enter on a Browse row
						// outside the search box": land on the repo
						// overview TUI, not straight on the PRs.
						m.next = &Action{Kind: "repo", Project: it.r.Project, Slug: it.r.Slug}
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
		case key.Matches(msg, m.keys.ClearStatus):
			// Clear any transient toast / error so it stops shadowing
			// the help bar at the bottom of the screen.
			m.status = ""
			m.err = nil
			return m, nil
		case key.Matches(msg, m.keys.Help):
			m.help.ShowAll = !m.help.ShowAll
			m.layout()
			return m, nil
		case key.Matches(msg, m.keys.Settings):
			// Open the universal settings overlay (theme, editor
			// modes, …). Same surface as the PR settings overlay so
			// users land on a familiar list no matter where they
			// triggered it from.
			m.settingsOpen = true
			m.settings.SetSize(m.width, m.height-4)
			return m, nil
		case key.Matches(msg, m.keys.Tab):
			m.tab = (m.tab + 1) % homeTab(len(allTabs))
			// Reset pane focus to the row list when leaving the
			// dashboard so re-entering from another tab doesn't
			// strand the user in the preview pane.
			m.dashFocus = ""
			return m, m.refreshPreviewForTab()
		case key.Matches(msg, m.keys.FocusPane):
			// Only the dashboard tab has two scroll surfaces worth
			// toggling between (row list ↔ markdown preview). The
			// favourites / browse tabs only have a single list, so
			// the toggle is a no-op there.
			if m.tab == tabDashboard {
				if m.dashFocus == "right" {
					m.dashFocus = ""
				} else {
					m.dashFocus = "right"
				}
			}
			return m, nil
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
				m.status = fmt.Sprintf("%sremoved %s/%s from favourites", theme.OKPrefix(), project, slug)
			} else {
				_ = config.AddFavourite(config.FavRepo{Host: host, Project: project, Slug: slug, Name: name})
				m.status = fmt.Sprintf("%sadded %s/%s to favourites", theme.OKPrefix(), project, slug)
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
			m.next = &Action{Kind: "prs", Project: project, Slug: slug}
			return m, tea.Quit
		case key.Matches(msg, m.keys.Enter):
			// Enter is the consistent "open this row" action across
			// every tab — repo rows land on the repo overview TUI
			// (README + recent PRs + builds) so the user gets the
			// same view they'd get from `bb repo` / `bb .`. PR rows
			// (only present on the dashboard) drill straight into
			// the PR TUI because the row already represents a PR.
			// The "p" shortcut is one keystroke away on the repo
			// overview if the user wants the bare PR list.
			switch m.tab {
			case tabDashboard:
				// Dashboard mixes PR rows and repo rows in one
				// flat cursor — branch on row kind so each lands
				// on the surface that actually matches what the
				// row represents.
				if r := m.selectedDashRow(); r != nil {
					kind := "prs"
					if r.kind == "repo" {
						kind = "repo"
					}
					m.next = &Action{Kind: kind, Project: r.project(), Slug: r.slug()}
					return m, tea.Quit
				}
			case tabFavourites:
				// Favourites and Browse rows are always repos, so
				// Enter routes to the repo overview TUI for
				// parity with the dashboard's repo rows. The
				// "p → PR TUI" shortcut is still one keystroke
				// away once the user has landed on the overview.
				if it, ok := m.favs.SelectedItem().(repoBrowseItem); ok {
					m.next = &Action{Kind: "repo", Project: it.r.Project, Slug: it.r.Slug}
					return m, tea.Quit
				}
			case tabBrowse:
				if it, ok := m.browse.SelectedItem().(repoBrowseItem); ok {
					m.next = &Action{Kind: "repo", Project: it.r.Project, Slug: it.r.Slug}
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
				_ = sysutil.OpenInBrowser(url)
			}
			return m, nil
		}

		// Forward unhandled keys to the focused list / preview.
		switch m.tab {
		case tabDashboard:
			// Right pane focused: send navigation / paging keys to
			// the README viewport so the user can scroll through a
			// long markdown body. The viewport handles up/down/k/j,
			// pgup/pgdn, and home/end natively.
			if m.dashFocus == "right" {
				var cmd tea.Cmd
				m.preview, cmd = m.preview.Update(msg)
				return m, cmd
			}
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

func (m homeModel) View() string {
	if m.err != nil {
		return theme.StatusErr.Render("error: "+m.err.Error()) + "\n\npress q to quit"
	}
	if m.width == 0 {
		return "" // wait for first WindowSizeMsg before painting.
	}

	// Settings overlay replaces the body so the user has the whole
	// frame to navigate toggles. Header chrome stays so they
	// remember where they came from. Esc returns to the dashboard.
	if m.settingsOpen {
		settingsHeader := theme.TitleBar("SETTINGS",
			theme.TitleChipDim.Render("persisted to ~/.config/bb/config.yml"))
		footer := m.help.View(m.keys)
		statusLine := theme.RenderStatusLine(m.loading, m.spinner.View(), m.status)
		return settingsHeader + "\n" + m.settings.View() + "\n" +
			theme.JoinFooter(statusLine, footer)
	}

	header := theme.TitleBar("BB · HOME",
		theme.TitleChip.Render(m.svc.Host()),
		theme.TitleChipDim.Render("tab to switch · / to search · ? for help"),
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
					theme.TitleChip.Render("f")+
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
				homeMuted.Render("No repos matched ")+theme.TitleChipWarn.Render(m.browseQ)+homeMuted.Render("."))
		case m.browseQ == "" && len(m.browse.Items()) == 0:
			listView = card("245", listInnerW, browseListInnerH,
				homeMuted.Render("Type to search across all repos.\n\nResults stream in as you type."))
		default:
			listView = m.browse.View()
		}
		leftInner = m.searchBox(listInnerW) + "\n" + listView
	}

	// Border colour follows pane focus on the dashboard tab so the
	// user can see which pane consumes scroll input. Other tabs only
	// have a left list, so the right preview stays muted.
	leftFocused := !(m.tab == tabDashboard && m.dashFocus == "right")
	leftPane := paneBorder(leftFocused, leftW, contentH).Render(leftInner)
	rightPane := paneBorder(!leftFocused, rightW, contentH).Render(m.preview.View())

	// Subtle vertical separator between panes.
	sepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	sepCol := sepStyle.Render(strings.TrimRight(strings.Repeat(theme.VerticalRule()+"\n", contentH), "\n"))

	body := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, sepCol, rightPane)

	footer := m.help.View(m.keys)
	statusLine := theme.RenderStatusLine(m.loading, m.spinner.View(), m.status)

	return header + "\n" + tabs + "\n" + body + "\n" + theme.JoinFooter(statusLine, footer)
}
