// Package tui contains Bubble Tea models for bb's interactive mode.
//
// PRs() returns a runnable program that lets the user browse pull requests
// for a given (host, project, slug). Keys:
//
//	↑/↓, j/k        navigate the list
//	enter           focus / refresh detail pane
//	d               open the diff for the selected PR
//	o               open in browser
//	r               refresh
//	s               cycle state filter (OPEN → MERGED → DECLINED → ALL)
//	?               toggle help
//	q, esc          quit / back
package tui

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hugo/bb/internal/api"
)

// PRs launches the interactive pull-requests TUI.
func PRs(svc api.Service, project, slug string) error {
	m := newPRModel(svc, project, slug)
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

// ---------- model ----------

type viewMode int

const (
	viewList viewMode = iota
	viewDiff
)

type keyMap struct {
	Up, Down       key.Binding
	Enter          key.Binding
	Diff           key.Binding
	Open           key.Binding
	Refresh        key.Binding
	State          key.Binding
	Help           key.Binding
	Back, Quit     key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Enter:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "view")),
		Diff:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "diff")),
		Open:    key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open in browser")),
		Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		State:   key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "cycle state")),
		Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Enter, k.Diff, k.Open, k.Refresh, k.State, k.Help, k.Quit}
}
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Enter},
		{k.Diff, k.Open, k.Refresh, k.State},
		{k.Help, k.Back, k.Quit},
	}
}

type prItem struct{ pr api.PullRequest }

func (i prItem) FilterValue() string { return i.pr.Title }
func (i prItem) Title() string {
	return fmt.Sprintf("#%d  %s", i.pr.ID, i.pr.Title)
}
func (i prItem) Description() string {
	return fmt.Sprintf("%s · %s → %s · %s", i.pr.State, i.pr.SourceRef, i.pr.TargetRef, i.pr.Author)
}

type prsLoadedMsg struct{ prs []api.PullRequest }
type diffLoadedMsg struct {
	id   int
	diff string
}
type errMsg struct{ err error }

type model struct {
	svc     api.Service
	project string
	slug    string
	state   string

	mode viewMode

	list     list.Model
	detail   viewport.Model
	diff     viewport.Model
	spinner  spinner.Model
	help     help.Model
	keys     keyMap
	loading  bool
	err      error
	width    int
	height   int
	diffID   int
}

func newPRModel(svc api.Service, project, slug string) model {
	delegate := list.NewDefaultDelegate()
	l := list.New(nil, delegate, 0, 0)
	l.Title = fmt.Sprintf("Pull Requests · %s/%s", project, slug)
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.Styles.Title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))

	return model{
		svc: svc, project: project, slug: slug,
		state:    "OPEN",
		list:     l,
		detail:   viewport.New(0, 0),
		diff:     viewport.New(0, 0),
		spinner:  sp,
		help:     help.New(),
		keys:     defaultKeys(),
		loading:  true,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.fetchPRs())
}

func (m *model) fetchPRs() tea.Cmd {
	return func() tea.Msg {
		prs, err := m.svc.ListPRs(m.project, m.slug, m.state, 100)
		if err != nil {
			return errMsg{err}
		}
		return prsLoadedMsg{prs}
	}
}

func (m *model) fetchDiff(id int) tea.Cmd {
	return func() tea.Msg {
		d, err := m.svc.PRDiff(m.project, m.slug, id)
		if err != nil {
			return errMsg{err}
		}
		return diffLoadedMsg{id: id, diff: d}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

	case prsLoadedMsg:
		m.loading = false
		items := make([]list.Item, 0, len(msg.prs))
		for _, p := range msg.prs {
			items = append(items, prItem{p})
		}
		m.list.SetItems(items)
		m.updateDetail()
		return m, nil

	case diffLoadedMsg:
		m.loading = false
		m.diff.SetContent(colorizeDiff(msg.diff))
		m.diff.GotoTop()
		m.diffID = msg.id
		m.mode = viewDiff
		return m, nil

	case errMsg:
		m.loading = false
		m.err = msg.err
		return m, nil

	case tea.KeyMsg:
		// Filtering input: don't intercept keys.
		if m.list.FilterState() == list.Filtering {
			break
		}

		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Back):
			if m.mode == viewDiff {
				m.mode = viewList
				return m, nil
			}
			return m, tea.Quit
		case key.Matches(msg, m.keys.Help):
			m.help.ShowAll = !m.help.ShowAll
			m.layout()
			return m, nil
		}

		if m.mode == viewDiff {
			var cmd tea.Cmd
			m.diff, cmd = m.diff.Update(msg)
			return m, cmd
		}

		switch {
		case key.Matches(msg, m.keys.Refresh):
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, m.fetchPRs())
		case key.Matches(msg, m.keys.State):
			m.state = nextState(m.state)
			m.list.Title = fmt.Sprintf("Pull Requests · %s/%s · %s", m.project, m.slug, m.state)
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, m.fetchPRs())
		case key.Matches(msg, m.keys.Diff):
			if it, ok := m.list.SelectedItem().(prItem); ok {
				m.loading = true
				return m, tea.Batch(m.spinner.Tick, m.fetchDiff(it.pr.ID))
			}
		case key.Matches(msg, m.keys.Open):
			if it, ok := m.list.SelectedItem().(prItem); ok && it.pr.WebURL != "" {
				_ = openInBrowser(it.pr.WebURL)
			}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	m.updateDetail()
	return m, cmd
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

func (m *model) layout() {
	helpHeight := lipgloss.Height(m.help.View(m.keys))
	listW := m.width / 2
	if listW < 32 {
		listW = m.width
	}
	detailW := m.width - listW - 2
	contentH := m.height - helpHeight - 1

	m.list.SetSize(listW, contentH)
	m.detail.Width = detailW
	m.detail.Height = contentH
	m.diff.Width = m.width
	m.diff.Height = m.height - helpHeight - 1
}

func (m model) View() string {
	if m.err != nil {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("error: "+m.err.Error()) +
			"\n\npress q to quit"
	}

	var body string
	switch m.mode {
	case viewDiff:
		header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")).
			Render(fmt.Sprintf("Diff · PR #%d", m.diffID))
		body = header + "\n" + m.diff.View()
	default:
		left := lipgloss.NewStyle().Padding(0, 1).Render(m.list.View())
		right := lipgloss.NewStyle().Padding(0, 1).Render(m.detail.View())
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	}

	help := m.help.View(m.keys)
	if m.loading {
		help = m.spinner.View() + " loading…  " + help
	}
	return body + "\n" + help
}

// ---------- helpers ----------

func nextState(s string) string {
	switch strings.ToUpper(s) {
	case "OPEN":
		return "MERGED"
	case "MERGED":
		return "DECLINED"
	case "DECLINED":
		return "ALL"
	default:
		return "OPEN"
	}
}

func openInBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler"}
	default:
		cmd = "xdg-open"
	}
	args = append(args, url)
	return exec.Command(cmd, args...).Start()
}

// humanTime is a tiny relative-time formatter.
func humanTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// styleState mirrors the styling used elsewhere.
func styleState(s string) string {
	switch strings.ToUpper(s) {
	case "OPEN", "INPROGRESS", "PENDING":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(s)
	case "MERGED", "SUCCESSFUL", "SUCCESS":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(s)
	case "DECLINED", "FAILED", "CANCELLED", "STOPPED":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(s)
	}
	return s
}

// colorizeDiff is duplicated here (small) to avoid coupling cmd ↔ tui.
func colorizeDiff(diff string) string {
	add := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	del := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	hunk := lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	meta := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("13"))
	var b strings.Builder
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") ||
			strings.HasPrefix(line, "diff ") || strings.HasPrefix(line, "index "):
			b.WriteString(meta.Render(line))
		case strings.HasPrefix(line, "@@"):
			b.WriteString(hunk.Render(line))
		case strings.HasPrefix(line, "+"):
			b.WriteString(add.Render(line))
		case strings.HasPrefix(line, "-"):
			b.WriteString(del.Render(line))
		default:
			b.WriteString(line)
		}
		b.WriteByte('\n')
	}
	return b.String()
}
