// Package tui — repository overview screen.
//
// The repo TUI is what `bb repo` and `bb .` open: a single-screen
// dashboard for one repository. Header chrome on top (project / slug,
// default branch, web URL), a scrollable README on the left, a quick
// summary on the right with the most recent open PRs and the latest
// builds for the default branch. Useful keys: p jumps into the full
// PR TUI, S opens repository settings, o opens the repo in a browser,
// c clones via git.
package repo

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hugs7/bitbucket-cli/internal/api"
	"github.com/hugs7/bitbucket-cli/internal/strutil"
	"github.com/hugs7/bitbucket-cli/internal/sysutil"
	"github.com/hugs7/bitbucket-cli/internal/tui/preview"
	"github.com/hugs7/bitbucket-cli/internal/tui/settings"
	"github.com/hugs7/bitbucket-cli/internal/tui/theme"
)

// RepoAction mirrors HomeAction: returned to the caller when the user
// wants to launch a different sub-TUI. Nil means a clean quit.
type RepoAction struct {
	Kind    string // "prs" or "settings"
	Project string
	Slug    string
}

// Repo launches the single-repo TUI. It returns a RepoAction so the
// caller can chain into another TUI (e.g. the PR browser) without
// dropping back to the shell.
func Repo(svc api.Service, project, slug string) (*RepoAction, error) {
	m := newRepoModel(svc, project, slug)
	final, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	if err != nil {
		return nil, err
	}
	rm := final.(repoModel)
	return rm.next, nil
}

type repoKeys struct {
	Up, Down, OpenPRs, RepoSettings, Open, Clone, Settings, Help, Quit, Back key.Binding
}

func defaultRepoKeys() repoKeys {
	return repoKeys{
		Up:           key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "scroll up")),
		Down:         key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "scroll down")),
		OpenPRs:      key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "PRs")),
		RepoSettings: key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "repo settings")),
		Open:         key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "browser")),
		Clone:        key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "clone")),
		Settings:     key.NewBinding(key.WithKeys(","), key.WithHelp(",", "settings")),
		Help:         key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:         key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Back:         key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	}
}

func (k repoKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.OpenPRs, k.RepoSettings, k.Open, k.Clone, k.Settings, k.Help, k.Quit}
}
func (k repoKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.OpenPRs, k.RepoSettings},
		{k.Open, k.Clone, k.Settings, k.Help, k.Back, k.Quit},
	}
}

type repoModel struct {
	svc           api.Service
	project, slug string

	repo    *api.Repo
	readme  string
	prs     []api.PullRequest
	builds  []api.Build
	loading int // outstanding fetches

	preview viewport.Model
	spinner spinner.Model
	help    help.Model
	keys    repoKeys

	// settings overlay (toggled with `,`). When `settingsOpen` is
	// true the overlay owns the keymap and the underlying view is
	// hidden behind the panel.
	settings     settings.Model
	settingsOpen bool

	width, height int
	status        string
	err           error

	next *RepoAction
}

type repoLoadedMsg struct{ repo *api.Repo }
type repoReadmeMsg struct{ body string }
type repoPRsMsg struct{ prs []api.PullRequest }
type repoBuildsMsg struct{ builds []api.Build }
type repoErrMsg struct{ err error }

func newRepoModel(svc api.Service, project, slug string) repoModel {
	theme.Init()
	sp := spinner.New()
	// |/-\ under 3270 to match old TTY cadence; braille dot otherwise.
	if theme.Mainframe() {
		sp.Spinner = spinner.Line
	} else {
		sp.Spinner = spinner.Dot
	}
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	return repoModel{
		svc:      svc,
		project:  project,
		slug:     slug,
		keys:     defaultRepoKeys(),
		spinner:  sp,
		help:     help.New(),
		preview:  viewport.New(0, 0),
		settings: settings.New(),
		loading:  1, // we always start by fetching repo metadata
	}
}

func (m repoModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.fetchRepo())
}

func (m *repoModel) fetchRepo() tea.Cmd {
	return func() tea.Msg {
		r, err := m.svc.GetRepo(m.project, m.slug)
		if err != nil {
			return repoErrMsg{err}
		}
		return repoLoadedMsg{r}
	}
}
func (m *repoModel) fetchReadme() tea.Cmd {
	return func() tea.Msg {
		body, _ := m.svc.GetReadme(m.project, m.slug)
		return repoReadmeMsg{body}
	}
}
func (m *repoModel) fetchPRs() tea.Cmd {
	return func() tea.Msg {
		prs, err := m.svc.ListPRs(m.project, m.slug, "OPEN", 10)
		if err != nil {
			return repoPRsMsg{}
		}
		return repoPRsMsg{prs}
	}
}
func (m *repoModel) fetchBuilds(ref string) tea.Cmd {
	return func() tea.Msg {
		if ref == "" {
			return repoBuildsMsg{}
		}
		b, _ := m.svc.ListBuildsForRef(m.project, m.slug, ref, 5)
		return repoBuildsMsg{b}
	}
}

func (m repoModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.loading > 0 {
			return m, cmd
		}
		return m, nil

	case repoLoadedMsg:
		m.repo = msg.repo
		// Fan out the three follow-on fetches in parallel: the
		// README, recent open PRs, and recent default-branch builds.
		// loading is bumped so the spinner/footer count stays
		// truthful until each one replies.
		m.loading += 3
		ref := ""
		if m.repo != nil {
			ref = m.repo.DefaultRef
		}
		m.loading-- // repo fetch itself is now complete
		m.refreshPreview()
		return m, tea.Batch(m.spinner.Tick, m.fetchReadme(), m.fetchPRs(), m.fetchBuilds(ref))

	case repoReadmeMsg:
		m.readme = msg.body
		m.loading--
		m.refreshPreview()
		return m, nil

	case repoPRsMsg:
		m.prs = msg.prs
		m.loading--
		m.refreshPreview()
		return m, nil

	case repoBuildsMsg:
		m.builds = msg.builds
		m.loading--
		m.refreshPreview()
		return m, nil

	case repoErrMsg:
		m.err = msg.err
		m.loading = 0
		return m, nil

	case tea.KeyMsg:
		// Settings overlay owns all keys while open; esc closes,
		// everything else routes through the overlay's list.
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
		switch {
		case key.Matches(msg, m.keys.Quit), key.Matches(msg, m.keys.Back):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Help):
			m.help.ShowAll = !m.help.ShowAll
			m.layout()
			return m, nil
		case key.Matches(msg, m.keys.OpenPRs):
			m.next = &RepoAction{Kind: "prs", Project: m.project, Slug: m.slug}
			return m, tea.Quit
		case key.Matches(msg, m.keys.RepoSettings):
			m.next = &RepoAction{Kind: "settings", Project: m.project, Slug: m.slug}
			return m, tea.Quit
		case key.Matches(msg, m.keys.Open):
			if m.repo != nil && m.repo.WebURL != "" {
				_ = sysutil.OpenInBrowser(m.repo.WebURL)
				m.status = theme.OKPrefix() + "opened in browser"
			}
			return m, nil
		case key.Matches(msg, m.keys.Clone):
			return m, m.cloneRepo()
		case key.Matches(msg, m.keys.Settings):
			// Open the universal settings overlay (theme, editor
			// modes, …). Sized to the inner viewport so it sits
			// neatly above the help bar.
			m.settingsOpen = true
			m.settings.SetSize(m.width, m.height-4)
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.preview, cmd = m.preview.Update(msg)
	return m, cmd
}

// cloneRepo suspends the program and runs `git clone` so the user
// gets standard git progress output and any auth prompts directly.
func (m *repoModel) cloneRepo() tea.Cmd {
	if m.repo == nil || m.repo.CloneHTTPS == "" {
		m.status = theme.ErrPrefix() + "no clone URL available yet"
		return nil
	}
	url := m.repo.CloneHTTPS
	return tea.ExecProcess(exec.Command("git", "clone", url), func(err error) tea.Msg {
		if err != nil {
			return repoErrMsg{fmt.Errorf("git clone: %w", err)}
		}
		return statusMsg{theme.OKPrefix() + "cloned " + url}
	})
}

// statusMsg is a tiny generic toast carrier — used by exec-based
// flows where the result is purely a status string.
type statusMsg struct{ text string }

func (m repoModel) View() string {
	if m.err != nil {
		return theme.StatusErr.Render("error: "+m.err.Error()) + "\n\npress q to quit"
	}

	var body string
	if m.settingsOpen {
		// Replace the body with the settings overlay so the user
		// has the whole frame to navigate toggles. Header chrome
		// stays so they remember which screen they came from.
		body = theme.TitleBar("SETTINGS",
			theme.TitleChipDim.Render("persisted to ~/.config/bb/config.yml")) +
			"\n" + m.settings.View()
	} else {
		header := theme.TitleBar(
			fmt.Sprintf("REPO · %s/%s", m.project, m.slug),
			m.headerChips()...,
		)
		left := lipgloss.NewStyle().Padding(0, 1).Render(m.preview.View())
		right := lipgloss.NewStyle().Padding(0, 1).Render(m.rightPane())
		body = header + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	}

	footer := m.help.View(m.keys)
	statusLine := theme.RenderStatusLine(m.loading > 0, m.spinner.View(), m.status)
	return body + "\n" + theme.JoinFooter(statusLine, footer)
}

// headerChips builds the small context chips shown after the title:
// default branch and web URL when available.
func (m repoModel) headerChips() []string {
	var chips []string
	if m.repo != nil {
		if m.repo.DefaultRef != "" {
			chips = append(chips, theme.TitleChip.Render(m.repo.DefaultRef))
		}
		if m.repo.WebURL != "" {
			chips = append(chips, theme.TitleChipDim.Render(m.repo.WebURL))
		}
	}
	return chips
}

// layout sizes the README viewport and the right summary pane to the
// terminal width. The split is roughly 65/35 because READMEs benefit
// from a wider column than PR / build summaries.
func (m *repoModel) layout() {
	if m.width == 0 || m.height == 0 {
		return
	}
	footerH := lipgloss.Height(m.help.View(m.keys))
	headerH := 2
	innerH := m.height - headerH - footerH - 1
	if innerH < 5 {
		innerH = 5
	}
	leftW := (m.width * 65) / 100
	if leftW < 30 {
		leftW = 30
	}
	m.preview.Width = leftW - 2
	m.preview.Height = innerH
	m.refreshPreview()
}

// refreshPreview re-renders the README content into the left
// viewport. Called whenever the README, repo metadata, or window
// size changes.
func (m *repoModel) refreshPreview() {
	// Pick the muted placeholder shown while we have no README to
	// display: "Loading…" while a fetch is still in flight, the
	// "no README" hint once everything has come back empty.
	fallback := "(no README found on default branch)"
	if m.loading > 0 && m.readme == "" {
		fallback = "Loading README…"
	}
	desc := ""
	if m.repo != nil {
		desc = m.repo.Description
	}
	m.preview.SetContent(preview.Body(m.readme, desc, m.preview.Width, fallback))
}

// rightPane composes the right summary column: a list of recent open
// PRs and the most recent builds for the default branch.
func (m repoModel) rightPane() string {
	var sb strings.Builder

	sb.WriteString(theme.TitleBadge.Render(" PULL REQUESTS "))
	sb.WriteString("\n")
	if len(m.prs) == 0 {
		sb.WriteString(theme.TitleChipDim.Render("  no open PRs"))
	} else {
		for _, p := range m.prs {
			line := fmt.Sprintf("  #%-4d  %s", p.ID, strutil.Truncate(p.Title, m.rightWidth()-12))
			sb.WriteString(line)
			sb.WriteString("\n")
			meta := fmt.Sprintf("        %s → %s · %s",
				lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Render(p.SourceRef),
				lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Render(p.TargetRef),
				lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Render(p.Author),
			)
			sb.WriteString(meta)
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\n")

	sb.WriteString(theme.TitleBadge.Render(" RECENT BUILDS "))
	sb.WriteString("\n")
	if len(m.builds) == 0 {
		sb.WriteString(theme.TitleChipDim.Render("  no builds for default branch"))
	} else {
		for _, b := range m.builds {
			dot := theme.BuildDot(b.State)
			name := b.Name
			if name == "" {
				name = b.ID
			}
			sb.WriteString(fmt.Sprintf("  %s %s · %s\n",
				dot, strutil.Truncate(name, m.rightWidth()-16), theme.StyleState(b.State)))
		}
	}
	return sb.String()
}

// rightWidth returns the usable width of the right column.
func (m repoModel) rightWidth() int {
	if m.width == 0 {
		return 40
	}
	leftW := (m.width * 65) / 100
	if leftW < 30 {
		leftW = 30
	}
	return m.width - leftW - 4
}
