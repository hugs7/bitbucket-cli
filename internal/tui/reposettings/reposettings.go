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
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hugs7/bitbucket-cli/internal/api"
	"github.com/hugs7/bitbucket-cli/internal/strutil"
	"github.com/hugs7/bitbucket-cli/internal/tui/theme"
)

// Run opens repository settings and reports whether Back requests its parent
// repository overview. Quit returns false, regardless of the entry point.
func Run(svc api.Service, project, slug string) (backToRepo bool, err error) {
	final, err := tea.NewProgram(newModel(svc, project, slug), tea.WithAltScreen()).Run()
	if err != nil {
		return false, err
	}
	return final.(model).backToRepo, nil
}

type panel struct {
	title    string
	section  string
	editable bool
}

var panels = []panel{
	{title: "Repository details", section: "Repository details", editable: false},
	{title: "Default branch", section: "Repository details", editable: false},
	{title: "Repository permissions", section: "Security", editable: false},
	{title: "Group permissions", section: "Security", editable: false},
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
	Up, Down, Left, Right, Enter, Add, Delete, Edit, Minimum, Refresh, Back, Quit, Help key.Binding
	SwitchPane, PageUp, PageDown, Top, Bottom                                           key.Binding
}

func defaultKeys() keys {
	return keys{
		Up:         key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:       key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Left:       key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "panels")),
		Right:      key.NewBinding(key.WithKeys("right", "l", "enter"), key.WithHelp("→/enter", "open")),
		Enter:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open / submit")),
		Add:        key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add")),
		Delete:     key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
		Edit:       key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit JSON")),
		Minimum:    key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "min approvals")),
		Refresh:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Back:       key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Quit:       key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Help:       key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		SwitchPane: key.NewBinding(key.WithKeys("tab", "shift+tab"), key.WithHelp("tab", "switch pane")),
		PageUp:     key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "scroll page up")),
		PageDown:   key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "scroll page down")),
		Top:        key.NewBinding(key.WithKeys("home"), key.WithHelp("home", "top of details")),
		Bottom:     key.NewBinding(key.WithKeys("end"), key.WithHelp("end", "bottom of details")),
	}
}

func (k keys) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Right, k.Edit, k.Minimum, k.Add, k.Delete, k.Refresh, k.Back, k.Help, k.Quit}
}

func (k keys) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Left, k.Right, k.Enter},
		{k.SwitchPane, k.PageUp, k.PageDown, k.Top, k.Bottom},
		{k.Edit, k.Minimum, k.Add, k.Delete, k.Refresh, k.Back, k.Help, k.Quit},
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

	repo       *api.Repo
	hooks      []api.Webhook
	settings   *api.RepoSettings
	loading    int
	status     string
	err        error
	mode       mode
	backToRepo bool
	panelIdx   int
	hookIdx    int
	edit       settingsEditState

	addURL         string
	addEvents      string
	addDescription string
	input          textinput.Model

	width, height int
	spinner       spinner.Model
	help          help.Model
	content       viewport.Model
	keys          keys
}

type repoLoadedMsg struct{ repo *api.Repo }
type webhooksLoadedMsg struct{ hooks []api.Webhook }
type settingsLoadedMsg struct{ settings *api.RepoSettings }
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
		content: viewport.New(0, 0),
		loading: 3,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.fetchRepo(), m.fetchWebhooks(), m.fetchSettings())
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

func (m model) fetchSettings() tea.Cmd {
	return func() tea.Msg {
		settings, err := m.svc.RepoSettings(m.project, m.slug)
		if err != nil {
			return errMsg{fmt.Errorf("load repository settings: %w", err)}
		}
		return settingsLoadedMsg{settings}
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
		m.help.Width = msg.Width
		m.content.Width = m.contentWidth() - 2
		m.content.Height = m.bodyHeight()
		if m.mode == modeSettingJSON {
			m.edit.json.SetWidth(m.contentWidth() - 4)
			m.edit.json.SetHeight(max(5, m.bodyHeight()-7))
		}
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

	case settingsLoadedMsg:
		m.settings = msg.settings
		m.loading--
		return m, nil

	case settingReadMsg:
		if msg.err != nil {
			m.mode = modePanel
			m.status = theme.ErrPrefix() + msg.err.Error()
			return m, nil
		}
		if m.edit.minimum {
			count, err := api.RepoMinimumApprovals(msg.raw)
			if err != nil {
				m.mode = modePanel
				m.status = err.Error()
				return m, nil
			}
			m.edit.original = msg.raw
			m.mode = modeSettingMinimum
			m.input.SetValue(fmt.Sprint(count))
			m.input.Placeholder = "minimum approvals (0 disables the merge check)"
			m.input.Focus()
			m.status = fmt.Sprintf("Current minimum approving reviewers: %d (0 = disabled). Enter the new minimum.", count)
			return m, textinput.Blink
		}
		if m.edit.action == "delete" || m.edit.action == "enable" || m.edit.action == "disable" {
			m.mode = modeSettingConfirm
			m.edit.patch = msg.raw
			return m, nil
		}
		return m.openSettingJSON(msg.raw)

	case actionDoneMsg:
		m.loading = 3
		m.mode = modePanel
		m.clearAddState()
		if msg.err != nil {
			m.loading = 0
			m.status = theme.ErrPrefix() + msg.text + ": " + msg.err.Error()
			return m, nil
		}
		m.status = theme.OKPrefix() + msg.text
		if m.edit.resource == "details" {
			m.status += ". If renamed, reopen settings using the new repository slug."
		}
		return m, tea.Batch(m.spinner.Tick, m.fetchRepo(), m.fetchWebhooks(), m.fetchSettings())

	case errMsg:
		m.loading = 0
		m.err = msg.err
		m.status = theme.ErrPrefix() + msg.err.Error()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	if m.mode == modeSettingJSON {
		var cmd tea.Cmd
		m.edit.json, cmd = m.edit.json.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mode >= modeSettingID {
		return m.handleSettingKey(msg)
	}
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
		m.backToRepo = true
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.help.ShowAll = !m.help.ShowAll
		return m, nil
	case key.Matches(msg, m.keys.Refresh):
		m.loading = 3
		m.status = "refreshing settings"
		return m, tea.Batch(m.spinner.Tick, m.fetchRepo(), m.fetchWebhooks(), m.fetchSettings())
	case key.Matches(msg, m.keys.SwitchPane):
		if m.mode == modePanels {
			m.mode = modePanel
		} else {
			m.mode = modePanels
		}
		return m, nil
	}

	// Visible action shortcuts work immediately on the selected section. Page
	// keys also focus the details, without requiring an invisible Enter step.
	if key.Matches(msg, m.keys.Edit, m.keys.Minimum, m.keys.Add, m.keys.Delete,
		m.keys.PageUp, m.keys.PageDown, m.keys.Top, m.keys.Bottom) {
		m.mode = modePanel
	}
	if m.mode == modePanels {
		switch {
		case key.Matches(msg, m.keys.Up):
			m.panelIdx = (m.panelIdx + len(panels) - 1) % len(panels)
			m.content.GotoTop()
		case key.Matches(msg, m.keys.Down):
			m.panelIdx = (m.panelIdx + 1) % len(panels)
			m.content.GotoTop()
		case key.Matches(msg, m.keys.Right), key.Matches(msg, m.keys.Enter):
			m.mode = modePanel
		}
		return m, nil
	}

	if key.Matches(msg, m.keys.PageUp, m.keys.PageDown, m.keys.Top, m.keys.Bottom) {
		m.syncPanelContent()
		switch {
		case key.Matches(msg, m.keys.Top):
			m.content.GotoTop()
		case key.Matches(msg, m.keys.Bottom):
			m.content.GotoBottom()
		default:
			m.content, _ = m.content.Update(msg)
		}
		return m, nil
	}
	if key.Matches(msg, m.keys.Edit) {
		return m.beginSettingEdit("update")
	}
	if key.Matches(msg, m.keys.Minimum) && m.activePanel().title == "Merge checks" && api.CanEditRepoSettings(m.svc) && m.loading == 0 {
		m.edit = settingsEditState{resource: "pull-requests", action: "update", minimum: true}
		return m.loadSetting()
	}
	if m.activePanel().title != "Webhooks" {
		if key.Matches(msg, m.keys.Add) {
			return m.beginSettingEdit("create")
		}
		if key.Matches(msg, m.keys.Delete) {
			return m.beginSettingEdit("delete")
		}
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
		return m, nil
	}
	m.syncPanelContent()
	var cmd tea.Cmd
	m.content, cmd = m.content.Update(msg)
	return m, cmd
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
			m.mode = modeConfirmAddWebhook
			m.status = "Review webhook; y confirms, n/esc cancels."
			return m, nil
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
	case "y", "Y":
		id := m.hooks[m.hookIdx].ID
		m.mode = modeSettingSaving
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
	sections, details := "SECTIONS · Tab to focus", "DETAILS · active"
	if m.mode == modePanels {
		sections, details = "SECTIONS · active", "DETAILS · Tab/Enter to focus"
	} else if m.mode != modePanel {
		sections = "SECTIONS"
	}
	paneHeaders := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(m.panelWidth()).Padding(0, 1).Render(theme.TitleBadge.Render(sections)),
		lipgloss.NewStyle().Width(m.contentWidth()).Padding(0, 1).Render(theme.TitleBadge.Render(details)))
	body := lipgloss.JoinHorizontal(lipgloss.Top, m.renderPanels(), m.renderActivePanel())
	return header + "\n" + paneHeaders + "\n" + body + "\n" + m.footerStatus() + "\n" + m.footerHelp()
}

func (m model) renderPanels() string {
	w := m.panelWidth()
	style := lipgloss.NewStyle().Width(w).Padding(0, 1)
	var sb strings.Builder
	lastSection := ""
	selectedLine := 0
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
			selectedLine = strings.Count(sb.String(), "\n")
			prefix = "▸ "
			lineStyle = lineStyle.Foreground(lipgloss.Color("231")).Bold(true)
		}
		marker := ""
		if m.editable(p) {
			marker = " " + theme.TitleChip.Render("edit")
		}
		sb.WriteString(lineStyle.Render(prefix + p.title))
		sb.WriteString(marker)
		sb.WriteString("\n")
	}
	lines := strings.Split(sb.String(), "\n")
	start := max(0, selectedLine-m.bodyHeight()+2)
	return style.Height(m.bodyHeight()).Render(clampLines(strings.Join(lines[start:], "\n"), m.bodyHeight()))
}

func (m model) renderActivePanel() string {
	w := m.contentWidth()
	style := lipgloss.NewStyle().Width(w).Padding(0, 1)
	var body string
	if m.mode >= modeSettingID {
		body = m.renderSettingEdit()
		if m.mode == modeSettingJSON {
			return style.Height(m.bodyHeight()).Render(body)
		}
		m.content.Height = m.bodyHeight()
		m.content.SetContent(lipgloss.NewStyle().Width(w - 2).Render(body))
		return style.Height(m.bodyHeight()).Render(m.content.View())
	}
	m.syncPanelContent()
	body = m.content.View()
	if m.mode == modePanels || m.mode == modePanel {
		total := m.content.TotalLineCount()
		position := fmt.Sprintf("Lines %d–%d/%d", m.content.YOffset+1, min(total, m.content.YOffset+m.content.Height), total)
		if !m.content.AtBottom() {
			position += " · more below ↓"
		}
		body += "\n" + theme.TitleChipDim.Render(position)
	}
	return style.Height(m.bodyHeight()).Render(body)
}

// syncPanelContent uses identical wrapping for rendering and scrolling so
// long fields remain reachable and switching focus preserves the position.
func (m *model) syncPanelContent() {
	w := m.contentWidth()
	p := m.activePanel()
	var body string
	switch p.title {
	case "Repository details":
		body = m.renderRepoDetails(w)
	case "Webhooks":
		body = m.renderWebhooks(w)
	default:
		body = m.renderSettingsPanel(p, w)
	}
	m.content.Width = w - 2
	m.content.Height = m.bodyHeight()
	if m.mode == modePanels || m.mode == modePanel {
		m.content.Height-- // fixed scroll-position row
	}
	m.content.SetContent(lipgloss.NewStyle().Width(w - 2).Render(body))
}

func (m model) renderRepoDetails(w int) string {
	var sb strings.Builder
	sb.WriteString(theme.TitleBadge.Render(" REPOSITORY DETAILS "))
	sb.WriteString("\n\n")
	if api.CanEditRepoSettings(m.svc) {
		sb.WriteString("e edit name/description/public/forkable.\nRenaming may change the slug; reopen with its new slug.\n\n")
	} else {
		sb.WriteString("Read-only on Cloud.\n\n")
	}
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

func (m model) renderSettingsPanel(p panel, w int) string {
	var sb strings.Builder
	sb.WriteString(theme.TitleBadge.Render(" " + strings.ToUpper(p.title) + " "))
	sb.WriteString("\n\n")
	if m.editable(p) {
		sb.WriteString(settingPanelHelp(p.title))
		if p.title == "Merge checks" {
			sb.WriteString(" · m minimum approving reviewers")
		}
		sb.WriteString("\n\n")
	} else {
		sb.WriteString("Read-only / unsupported in this client.\n\n")
	}
	if m.settings == nil {
		sb.WriteString("Loading repository settings…")
		return sb.String()
	}
	panel, ok := m.settings.Panels[p.title]
	if !ok {
		sb.WriteString(theme.StatusInfo.Render("This panel did not return settings data."))
		return sb.String()
	}
	if panel.Hint != "" {
		sb.WriteString(lipgloss.NewStyle().Width(max(1, w-4)).Render(panel.Hint))
		sb.WriteString("\n\n")
	}
	if panel.Error != "" {
		sb.WriteString(theme.StatusErr.Width(max(1, w-4)).Render(panel.Error))
		sb.WriteString("\n")
		return sb.String()
	}
	if len(panel.Items) == 0 {
		sb.WriteString(theme.TitleChipDim.Render("No settings returned."))
		return sb.String()
	}
	for _, item := range panel.Items {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = "setting"
		}
		sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Render(title))
		sb.WriteString("\n")
		for _, field := range item.Fields {
			if strings.EqualFold(field.Value, title) {
				continue
			}
			sb.WriteString("  ")
			sb.WriteString(theme.TitleChipDim.Render(field.Name + ":"))
			sb.WriteString(" ")
			sb.WriteString(field.Value)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}
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
	if w < 32 {
		w = 32
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

func (m model) bodyHeight() int {
	if m.height == 0 {
		return 20
	}
	footerH := lipgloss.Height(m.footerHelp())
	statusH := lipgloss.Height(m.footerStatus())
	// Header + newline above body + footer/status line. Keep at least a
	// small viewport so long panels scroll instead of pushing the menu
	// and header out of the terminal.
	h := m.height - footerH - statusH - 3
	if h < 5 {
		return 5
	}
	return h
}

func clampLines(s string, max int) string {
	if max <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= max {
		return s
	}
	return strings.Join(lines[:max], "\n")
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
