// Package reposettings implements the repository settings TUI.
//
// The screen is intentionally panel-based because Bitbucket's repo
// settings surface is broad. Panels can be wired up incrementally while
// keeping one stable entry point from the CLI and repo overview TUI.
package reposettings

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hugs7/bitbucket-cli/internal/api"
	"github.com/hugs7/bitbucket-cli/internal/strutil"
	"github.com/hugs7/bitbucket-cli/internal/tui/theme"
)

// Run opens the repository settings screen for project/slug.
func Run(svc api.Service, project, slug string) error {
	_, err := tea.NewProgram(newModel(svc, project, slug), tea.WithAltScreen()).Run()
	return err
}

type panel struct {
	title    string
	section  string
	editable bool
}

var panels = []panel{
	{title: "Repository details", section: "Repository details", editable: false},
	{title: "Repository permissions", section: "Security", editable: false},
	{title: "Branch permissions", section: "Security", editable: false},
	{title: "Access keys", section: "Security", editable: false},
	{title: "HTTP access tokens", section: "Security", editable: false},
	{title: "Push log", section: "Security", editable: false},
	{title: "Audit log", section: "Security", editable: false},
	{title: "Branches", section: "Workflow", editable: false},
	{title: "Hooks", section: "Workflow", editable: false},
	{title: "Webhooks", section: "Workflow", editable: true},
	{title: "Jira issues", section: "Workflow", editable: false},
	{title: "Merge checks", section: "Pull requests", editable: false},
	{title: "Merge strategies", section: "Pull requests", editable: false},
	{title: "Code Insights", section: "Pull requests", editable: false},
	{title: "Default reviewers", section: "Pull requests", editable: false},
	{title: "Reviewer groups", section: "Pull requests", editable: false},
	{title: "Auto-decline", section: "Pull requests", editable: false},
	{title: "Description template", section: "Pull requests", editable: false},
	{title: "Required builds", section: "Pull requests", editable: false},
}

type keys struct {
	Up, Down, Left, Right, Enter, Add, Delete, Refresh, Back, Quit, Help key.Binding
}

func defaultKeys() keys {
	return keys{
		Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Left:    key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "panels")),
		Right:   key.NewBinding(key.WithKeys("right", "l", "enter"), key.WithHelp("→/enter", "open")),
		Enter:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open / submit")),
		Add:     key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add")),
		Delete:  key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
		Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	}
}

func (k keys) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Right, k.Add, k.Delete, k.Refresh, k.Back, k.Help, k.Quit}
}

func (k keys) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Left, k.Right, k.Enter},
		{k.Add, k.Delete, k.Refresh, k.Back, k.Help, k.Quit},
	}
}

type mode int

const (
	modePanels mode = iota
	modePanel
	modeAddURL
	modeAddEvents
	modeAddDescription
	modeConfirmDelete
)

type model struct {
	svc           api.Service
	project, slug string

	repo     *api.Repo
	hooks    []api.Webhook
	loading  int
	status   string
	err      error
	mode     mode
	panelIdx int
	hookIdx  int

	addURL         string
	addEvents      string
	addDescription string
	input          textinput.Model

	width, height int
	spinner       spinner.Model
	help          help.Model
	keys          keys
}

type repoLoadedMsg struct{ repo *api.Repo }
type webhooksLoadedMsg struct{ hooks []api.Webhook }
type actionDoneMsg struct {
	text string
	err  error
}
type errMsg struct{ err error }

func newModel(svc api.Service, project, slug string) model {
	theme.Init()
	sp := spinner.New()
	if theme.Mainframe() {
		sp.Spinner = spinner.Line
	} else {
		sp.Spinner = spinner.Dot
	}
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	ti := textinput.New()
	ti.Prompt = "> "
	return model{
		svc:     svc,
		project: project,
		slug:    slug,
		mode:    modePanels,
		spinner: sp,
		help:    help.New(),
		keys:    defaultKeys(),
		input:   ti,
		loading: 2,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.fetchRepo(), m.fetchWebhooks())
}

func (m model) fetchRepo() tea.Cmd {
	return func() tea.Msg {
		r, err := m.svc.GetRepo(m.project, m.slug)
		if err != nil {
			return errMsg{err}
		}
		return repoLoadedMsg{r}
	}
}

func (m model) fetchWebhooks() tea.Cmd {
	return func() tea.Msg {
		hooks, err := m.svc.ListWebhooks(m.project, m.slug)
		if err != nil {
			return errMsg{fmt.Errorf("load webhooks: %w", err)}
		}
		return webhooksLoadedMsg{hooks}
	}
}

func (m model) addWebhook() tea.Cmd {
	url := strings.TrimSpace(m.addURL)
	events := splitCSV(m.addEvents)
	description := strings.TrimSpace(m.addDescription)
	return func() tea.Msg {
		hook, err := m.svc.AddWebhook(m.project, m.slug, api.WebhookInput{
			URL:         url,
			Events:      events,
			Active:      true,
			Description: description,
		})
		if err != nil {
			return actionDoneMsg{text: "add webhook", err: err}
		}
		return actionDoneMsg{text: "created webhook " + hook.ID}
	}
}

func (m model) deleteWebhook(id string) tea.Cmd {
	return func() tea.Msg {
		err := m.svc.DeleteWebhook(m.project, m.slug, id)
		return actionDoneMsg{text: "deleted webhook " + id, err: err}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
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
		m.loading--
		return m, nil

	case webhooksLoadedMsg:
		m.hooks = msg.hooks
		m.loading--
		if m.hookIdx >= len(m.hooks) {
			m.hookIdx = len(m.hooks) - 1
		}
		if m.hookIdx < 0 {
			m.hookIdx = 0
		}
		return m, nil

	case actionDoneMsg:
		m.loading = 1
		m.mode = modePanel
		m.clearAddState()
		if msg.err != nil {
			m.loading = 0
			m.status = theme.ErrPrefix() + msg.text + ": " + msg.err.Error()
			return m, nil
		}
		m.status = theme.OKPrefix() + msg.text
		return m, tea.Batch(m.spinner.Tick, m.fetchWebhooks())

	case errMsg:
		m.loading = 0
		m.err = msg.err
		m.status = theme.ErrPrefix() + msg.err.Error()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeAddURL || m.mode == modeAddEvents || m.mode == modeAddDescription {
		return m.handleInputKey(msg)
	}
	if m.mode == modeConfirmDelete {
		return m.handleDeleteConfirmKey(msg)
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Back):
		if m.mode == modePanel {
			m.mode = modePanels
			return m, nil
		}
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.help.ShowAll = !m.help.ShowAll
		return m, nil
	case key.Matches(msg, m.keys.Refresh):
		m.loading = 2
		m.status = "refreshing settings"
		return m, tea.Batch(m.spinner.Tick, m.fetchRepo(), m.fetchWebhooks())
	}

	if m.mode == modePanels {
		switch {
		case key.Matches(msg, m.keys.Up):
			m.panelIdx = (m.panelIdx + len(panels) - 1) % len(panels)
		case key.Matches(msg, m.keys.Down):
			m.panelIdx = (m.panelIdx + 1) % len(panels)
		case key.Matches(msg, m.keys.Right), key.Matches(msg, m.keys.Enter):
			m.mode = modePanel
		}
		return m, nil
	}

	if m.activePanel().title == "Webhooks" {
		switch {
		case key.Matches(msg, m.keys.Left):
			m.mode = modePanels
		case key.Matches(msg, m.keys.Up):
			if len(m.hooks) > 0 {
				m.hookIdx = (m.hookIdx + len(m.hooks) - 1) % len(m.hooks)
			}
		case key.Matches(msg, m.keys.Down):
			if len(m.hooks) > 0 {
				m.hookIdx = (m.hookIdx + 1) % len(m.hooks)
			}
		case key.Matches(msg, m.keys.Add):
			m.mode = modeAddURL
			m.input.Placeholder = "https://example.com/hook"
			m.input.SetValue("")
			m.input.Focus()
			m.status = "webhook URL"
			return m, textinput.Blink
		case key.Matches(msg, m.keys.Delete):
			if len(m.hooks) == 0 {
				m.status = "no webhook selected"
				return m, nil
			}
			m.mode = modeConfirmDelete
			m.status = fmt.Sprintf("delete webhook %s? y/n", m.hooks[m.hookIdx].ID)
		}
		return m, nil
	}

	if key.Matches(msg, m.keys.Left) {
		m.mode = modePanels
	}
	return m, nil
}

func (m model) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Back):
		m.mode = modePanel
		m.clearAddState()
		m.status = "add webhook cancelled"
		return m, nil
	case key.Matches(msg, m.keys.Enter):
		switch m.mode {
		case modeAddURL:
			m.addURL = strings.TrimSpace(m.input.Value())
			if m.addURL == "" {
				m.status = theme.ErrPrefix() + "webhook URL is required"
				return m, nil
			}
			m.mode = modeAddEvents
			m.input.SetValue("")
			m.input.Placeholder = "repo:refs_changed,pr:opened"
			m.status = "comma-separated events"
			return m, textinput.Blink
		case modeAddEvents:
			m.addEvents = strings.TrimSpace(m.input.Value())
			if len(splitCSV(m.addEvents)) == 0 {
				m.status = theme.ErrPrefix() + "at least one event is required"
				return m, nil
			}
			m.mode = modeAddDescription
			m.input.SetValue("")
			m.input.Placeholder = "bb-webhook"
			m.status = "description (optional)"
			return m, textinput.Blink
		case modeAddDescription:
			m.addDescription = strings.TrimSpace(m.input.Value())
			m.input.Blur()
			m.loading = 1
			m.status = "creating webhook"
			return m, tea.Batch(m.spinner.Tick, m.addWebhook())
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) handleDeleteConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if len(m.hooks) == 0 {
		m.mode = modePanel
		return m, nil
	}
	switch msg.String() {
	case "y", "Y", "enter":
		id := m.hooks[m.hookIdx].ID
		m.mode = modePanel
		m.loading = 1
		m.status = "deleting webhook"
		return m, tea.Batch(m.spinner.Tick, m.deleteWebhook(id))
	case "n", "N", "esc":
		m.mode = modePanel
		m.status = "delete cancelled"
		return m, nil
	}
	return m, nil
}

func (m *model) clearAddState() {
	m.addURL = ""
	m.addEvents = ""
	m.addDescription = ""
	m.input.Blur()
	m.input.SetValue("")
}

func (m model) View() string {
	if m.err != nil && m.repo == nil && len(m.hooks) == 0 {
		return theme.StatusErr.Render("error: "+m.err.Error()) + "\n\npress q to quit"
	}

	header := theme.TitleBar(
		fmt.Sprintf("REPO SETTINGS · %s/%s", m.project, m.slug),
		theme.TitleChipDim.Render(m.svc.Host()),
	)
	body := lipgloss.JoinHorizontal(lipgloss.Top, m.renderPanels(), m.renderActivePanel())
	statusLine := theme.RenderStatusLine(m.loading > 0, m.spinner.View(), m.status)
	return header + "\n" + body + "\n" + theme.JoinFooter(statusLine, m.help.View(m.keys))
}

func (m model) renderPanels() string {
	w := m.panelWidth()
	style := lipgloss.NewStyle().Width(w).Padding(0, 1)
	var sb strings.Builder
	lastSection := ""
	for i, p := range panels {
		if p.section != lastSection {
			if lastSection != "" {
				sb.WriteString("\n")
			}
			sb.WriteString(theme.TitleBadge.Render(" " + strings.ToUpper(p.section) + " "))
			sb.WriteString("\n")
			lastSection = p.section
		}
		prefix := "  "
		lineStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		if i == m.panelIdx {
			prefix = "▸ "
			lineStyle = lineStyle.Foreground(lipgloss.Color("231")).Bold(true)
		}
		marker := ""
		if p.editable {
			marker = " " + theme.TitleChip.Render("edit")
		}
		sb.WriteString(lineStyle.Render(prefix + p.title))
		sb.WriteString(marker)
		sb.WriteString("\n")
	}
	return style.Render(sb.String())
}

func (m model) renderActivePanel() string {
	w := m.contentWidth()
	p := m.activePanel()
	style := lipgloss.NewStyle().Width(w).Padding(0, 1)
	var body string
	switch p.title {
	case "Repository details":
		body = m.renderRepoDetails(w)
	case "Webhooks":
		body = m.renderWebhooks(w)
	default:
		body = m.renderPlaceholder(p, w)
	}
	return style.Render(body)
}

func (m model) renderRepoDetails(w int) string {
	var sb strings.Builder
	sb.WriteString(theme.TitleBadge.Render(" REPOSITORY DETAILS "))
	sb.WriteString("\n\n")
	if m.repo == nil {
		sb.WriteString("Loading repository metadata…")
		return sb.String()
	}
	rows := [][2]string{
		{"Project/workspace", m.repo.Project},
		{"Slug", m.repo.Slug},
		{"Name", m.repo.Name},
		{"Default branch", m.repo.DefaultRef},
		{"Web URL", m.repo.WebURL},
		{"Clone HTTPS", m.repo.CloneHTTPS},
		{"Clone SSH", m.repo.CloneSSH},
	}
	for _, row := range rows {
		if row[1] == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf("%-16s %s\n", row[0]+":", strutil.Truncate(row[1], w-20)))
	}
	if strings.TrimSpace(m.repo.Description) != "" {
		sb.WriteString("\n")
		sb.WriteString(theme.TitleBadge.Render(" DESCRIPTION "))
		sb.WriteString("\n")
		sb.WriteString(m.repo.Description)
	}
	return sb.String()
}

func (m model) renderWebhooks(w int) string {
	var sb strings.Builder
	sb.WriteString(theme.TitleBadge.Render(" WEBHOOKS "))
	sb.WriteString("\n")
	sb.WriteString(theme.TitleChipDim.Render("a add · d delete · r refresh"))
	sb.WriteString("\n\n")

	if m.mode == modeAddURL || m.mode == modeAddEvents || m.mode == modeAddDescription {
		sb.WriteString(m.renderAddForm())
		sb.WriteString("\n\n")
	}
	if m.mode == modeConfirmDelete && len(m.hooks) > 0 {
		sb.WriteString(theme.StatusInfo.Render(fmt.Sprintf("Delete webhook %s? y/n", m.hooks[m.hookIdx].ID)))
		sb.WriteString("\n\n")
	}

	if len(m.hooks) == 0 {
		sb.WriteString(theme.TitleChipDim.Render("No webhooks configured."))
		return sb.String()
	}
	for i, h := range m.hooks {
		prefix := "  "
		lineStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		if i == m.hookIdx {
			prefix = "▸ "
			lineStyle = lineStyle.Foreground(lipgloss.Color("231")).Bold(true)
		}
		active := "disabled"
		if h.Active {
			active = "active"
		}
		title := h.Description
		if title == "" {
			title = h.ID
		}
		sb.WriteString(lineStyle.Render(prefix + strutil.Truncate(title, w-8)))
		sb.WriteString(" ")
		sb.WriteString(theme.TitleChipDim.Render(active))
		sb.WriteString("\n")
		sb.WriteString("    ")
		sb.WriteString(strutil.Truncate(h.URL, w-8))
		sb.WriteString("\n")
		if len(h.Events) > 0 {
			sb.WriteString("    ")
			sb.WriteString(strutil.Truncate(strings.Join(h.Events, ", "), w-8))
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func (m model) renderAddForm() string {
	var title string
	switch m.mode {
	case modeAddURL:
		title = "Webhook URL"
	case modeAddEvents:
		title = "Events (comma-separated)"
	case modeAddDescription:
		title = "Description (optional)"
	}
	return theme.TitleBadge.Render(" ADD WEBHOOK ") + "\n" + title + "\n" + m.input.View()
}

func (m model) renderPlaceholder(p panel, w int) string {
	var sb strings.Builder
	sb.WriteString(theme.TitleBadge.Render(" " + strings.ToUpper(p.title) + " "))
	sb.WriteString("\n\n")
	sb.WriteString("This settings panel is not wired up yet.\n\n")
	sb.WriteString("The repo-settings TUI is panel-based so this can be added without changing the entry point. ")
	sb.WriteString("For this panel we still need the Bitbucket Server/Data Center and Cloud REST endpoints plus field semantics.\n\n")
	sb.WriteString(theme.TitleChipDim.Render(strutil.Truncate("Press ←/h or esc to return to the panel list.", w-4)))
	return sb.String()
}

func (m model) activePanel() panel {
	if m.panelIdx < 0 || m.panelIdx >= len(panels) {
		return panels[0]
	}
	return panels[m.panelIdx]
}

func (m model) panelWidth() int {
	if m.width == 0 {
		return 34
	}
	w := (m.width * 35) / 100
	if w < 28 {
		w = 28
	}
	if w > 42 {
		w = 42
	}
	return w
}

func (m model) contentWidth() int {
	if m.width == 0 {
		return 80
	}
	w := m.width - m.panelWidth() - 4
	if w < 40 {
		w = 40
	}
	return w
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
