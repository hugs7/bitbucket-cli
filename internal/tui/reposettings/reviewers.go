package reposettings

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hugs7/bitbucket-cli/internal/api"
	"github.com/hugs7/bitbucket-cli/internal/strutil"
	"github.com/hugs7/bitbucket-cli/internal/tui/theme"
)

type reviewerMatcher struct {
	ID   string `json:"id"`
	Type struct {
		ID string `json:"id"`
	} `json:"type"`
}

type reviewerPerson struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

type reviewerRule struct {
	ID        int              `json:"id"`
	Source    reviewerMatcher  `json:"sourceRefMatcher"`
	Target    reviewerMatcher  `json:"targetRefMatcher"`
	Reviewers []reviewerPerson `json:"reviewers"`
	Required  int              `json:"requiredApprovals"`
	Scope     struct {
		Type string `json:"type"`
	} `json:"scope"`
}

// UnmarshalJSON accepts both response matcher names used by Server versions.
func (r *reviewerRule) UnmarshalJSON(raw []byte) error {
	type rule reviewerRule
	var value struct {
		rule
		SourceMatcher reviewerMatcher `json:"sourceMatcher"`
		TargetMatcher reviewerMatcher `json:"targetMatcher"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	*r = reviewerRule(value.rule)
	if r.Source.Type.ID == "" {
		r.Source = value.SourceMatcher
	}
	if r.Target.Type.ID == "" {
		r.Target = value.TargetMatcher
	}
	return nil
}

type reviewerEditor struct {
	rules       []reviewerRule
	selected    int
	fields      [4]textinput.Model
	field       int
	people      []reviewerPerson
	results     []api.User
	result      int
	searchFocus bool
	searchID    int
	matchers    [2]reviewerMatcher
}

type reviewerSearchMsg struct {
	id    int
	users []api.User
	err   error
}

// focusRow uses the configured palette and keeps a cursor for monochrome terminals.
func focusRow(text string, width int, active bool) string {
	style := lipgloss.NewStyle().Width(max(1, width))
	if active {
		style = style.Background(theme.Current.TitleBadgeBg).Foreground(theme.Current.TitleBadgeFg).Bold(true)
	}
	return style.Render(text)
}

func matcherLabel(m reviewerMatcher) string {
	if m.Type.ID == "ANY_REF" {
		return "Any branch"
	}
	label := strings.TrimPrefix(m.ID, "refs/heads/")
	if m.Type.ID != "BRANCH" {
		label += " (" + m.Type.ID + ")"
	}
	return label
}

func personLabel(p reviewerPerson) string {
	if p.DisplayName != "" {
		return p.DisplayName
	}
	if p.Name != "" {
		return p.Name
	}
	return fmt.Sprintf("User #%d", p.ID)
}

func peopleLabel(people []reviewerPerson) string {
	names := make([]string, 0, len(people))
	for _, p := range people {
		names = append(names, personLabel(p))
	}
	if len(names) == 0 {
		return "No reviewers selected"
	}
	return strings.Join(names, ", ")
}

func (m model) reviewerPanel() bool {
	return m.activePanel().title == "Default reviewers" && api.CanEditRepoSettings(m.svc)
}

func (m model) renderReviewerRules() string {
	if m.settings == nil {
		return "Loading reviewer rules…"
	}
	if err := m.settings.Panels["Default reviewers"].Error; err != "" {
		return theme.StatusErr.Render(err)
	}
	if len(m.reviewers.rules) == 0 {
		return "Default reviewers\n\nNo rules yet.\nPress a to add your first reviewer rule."
	}
	var rows []string
	for i, rule := range m.reviewers.rules {
		prefix := "  "
		if i == m.reviewers.selected {
			prefix = "▸ "
		}
		label := fmt.Sprintf("%s%s → %s\n  %s\n  Approvals required: %d", prefix, matcherLabel(rule.Source), matcherLabel(rule.Target), peopleLabel(rule.Reviewers), rule.Required)
		if rule.Scope.Type != "" && rule.Scope.Type != "REPOSITORY" {
			label += "\n  Inherited · read only"
		}
		rows = append(rows, focusRow(label, m.contentWidth()-2, i == m.reviewers.selected && m.mode == modePanel))
	}
	return "Default reviewers\nChoose a rule · Enter to edit\n\n" + strings.Join(rows, "\n\n")
}

func (m model) beginReviewerEdit(action string, advanced bool) (tea.Model, tea.Cmd) {
	if m.loading > 0 {
		m.status = "Wait for settings to finish loading."
		return m, nil
	}
	m.edit = settingsEditState{resource: "default-reviewers", action: action, reviewerForm: !advanced}
	if action == "create" {
		return m.openReviewerForm(settingCreateTemplate("default-reviewers"))
	}
	if len(m.reviewers.rules) == 0 {
		m.status = "No rule selected. Press a to add one."
		return m, nil
	}
	rule := m.reviewers.rules[m.reviewers.selected]
	if rule.Scope.Type != "" && rule.Scope.Type != "REPOSITORY" {
		m.status = "Inherited rule: edit it in the owning project's settings."
		return m, nil
	}
	m.edit.id = strconv.Itoa(rule.ID)
	return m.loadSetting()
}

// openReviewerForm keeps the complete original document; only changed fields
// are submitted, leaving branch models, reviewer groups and future fields intact.
func (m model) openReviewerForm(raw json.RawMessage) (tea.Model, tea.Cmd) {
	var fields map[string]json.RawMessage
	var rule reviewerRule
	if err := json.Unmarshal(raw, &fields); err != nil {
		m.status = err.Error()
		m.mode = modePanel
		return m, nil
	}
	if err := json.Unmarshal(raw, &rule); err != nil {
		m.status = err.Error()
		m.mode = modePanel
		return m, nil
	}
	for i, name := range []string{"source", "target"} {
		value := fields[name+"RefMatcher"]
		if len(value) == 0 {
			value = fields[name+"Matcher"]
		}
		if err := json.Unmarshal(value, &m.reviewers.matchers[i]); err != nil {
			m.status = "Cannot read branch matcher; use J for advanced JSON."
			m.mode = modePanel
			return m, nil
		}
	}
	m.edit.original = raw
	m.reviewers.people = append([]reviewerPerson{}, rule.Reviewers...)
	m.reviewers.field = 0
	for i := range m.reviewers.fields {
		input := textinput.New()
		input.Prompt = ""
		input.CharLimit = 0
		input.Placeholder = "Any branch (leave empty)"
		if i < 2 {
			matcher := m.reviewers.matchers[i]
			if matcher.Type.ID != "ANY_REF" {
				input.SetValue(strings.TrimPrefix(matcher.ID, "refs/heads/"))
			}
		}
		if i == 3 {
			input.SetValue(strconv.Itoa(rule.Required))
			input.Placeholder = "0"
		}
		m.reviewers.fields[i] = input
	}
	m.mode = modeReviewerForm
	m.status = ""
	return m, m.reviewers.fields[0].Focus()
}

func (m model) reviewerPatch() (json.RawMessage, error) {
	count, err := strconv.Atoi(strings.TrimSpace(m.reviewers.fields[3].Value()))
	if err != nil || count < 0 {
		return nil, fmt.Errorf("Required approvals must be a non-negative number.")
	}
	var original map[string]json.RawMessage
	_ = json.Unmarshal(m.edit.original, &original)
	patch := map[string]json.RawMessage{}
	for i, name := range []string{"source", "target"} {
		matcher := m.reviewers.matchers[i]
		if matcher.Type.ID != "ANY_REF" && matcher.Type.ID != "BRANCH" {
			continue
		}
		value := strings.TrimSpace(m.reviewers.fields[i].Value())
		before := strings.TrimPrefix(matcher.ID, "refs/heads/")
		if matcher.Type.ID == "ANY_REF" {
			before = ""
		}
		if value == before && m.edit.action == "update" {
			continue
		}
		id, kind, display := "any", "ANY_REF", "Any branch"
		if value != "" {
			if strings.ContainsAny(value, " *?[]\\~^:") {
				return nil, fmt.Errorf("Use an exact branch name, or leave it empty for any branch.")
			}
			id, kind, display = "refs/heads/"+strings.TrimPrefix(value, "refs/heads/"), "BRANCH", strings.TrimPrefix(value, "refs/heads/")
		}
		patch[name+"Matcher"], _ = json.Marshal(map[string]any{"id": id, "displayId": display, "type": map[string]string{"id": kind}})
	}
	// Preserve complete original reviewer objects if the selection is unchanged.
	var old []reviewerPerson
	_ = json.Unmarshal(original["reviewers"], &old)
	same := len(old) == len(m.reviewers.people)
	for i, person := range m.reviewers.people {
		if i >= len(old) || old[i].ID != person.ID || old[i].Name != person.Name {
			same = false
		}
	}
	if !same || m.edit.action == "create" {
		people := make([]map[string]any, 0, len(m.reviewers.people))
		for _, person := range m.reviewers.people {
			if person.ID <= 0 {
				return nil, fmt.Errorf("%s has no numeric user ID. Reselect them using search.", personLabel(person))
			}
			people = append(people, map[string]any{"id": person.ID})
		}
		patch["reviewers"], _ = json.Marshal(people)
	}
	if m.edit.action == "create" || string(original["requiredApprovals"]) != strconv.Itoa(count) {
		patch["requiredApprovals"] = json.RawMessage(strconv.Itoa(count))
	}
	if len(patch) == 0 {
		return nil, fmt.Errorf("No changes to save.")
	}
	return json.Marshal(patch)
}

func (m model) handleReviewerForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "esc" || key == "ctrl+c" {
		m.mode = modePanel
		m.status = "Changes cancelled; nothing written."
		return m, nil
	}
	if key == "ctrl+s" || (key == "enter" && m.reviewers.field == 4) {
		patch, err := m.reviewerPatch()
		if err != nil {
			m.status = err.Error()
			return m, nil
		}
		m.edit.patch = patch
		m.mode = modeSettingConfirm
		m.content.GotoTop()
		m.status = ""
		return m, nil
	}
	if key == "enter" && m.reviewers.field == 2 {
		m.mode = modeReviewerPeople
		m.reviewers.searchID++ // invalidate a search from a previously closed picker
		m.reviewers.searchFocus = true
		m.reviewers.results = nil
		for _, p := range m.reviewers.people {
			m.reviewers.results = append(m.reviewers.results, api.User{ID: p.ID, Username: p.Name, DisplayName: p.DisplayName})
		}
		m.reviewers.result = 0
		m.input.SetValue("")
		m.input.Placeholder = "Search by name or username"
		m.status = ""
		return m, m.input.Focus()
	}
	if key == "tab" || key == "down" || key == "enter" || key == "shift+tab" || key == "up" {
		for i := range m.reviewers.fields {
			m.reviewers.fields[i].Blur()
		}
		delta := 1
		if key == "shift+tab" || key == "up" {
			delta = -1
		}
		m.reviewers.field = (m.reviewers.field + delta + 5) % 5
		if m.reviewers.field < 4 && m.reviewers.field != 2 {
			return m, m.reviewers.fields[m.reviewers.field].Focus()
		}
		return m, nil
	}
	i := m.reviewers.field
	if i < 2 && m.reviewers.matchers[i].Type.ID != "ANY_REF" && m.reviewers.matchers[i].Type.ID != "BRANCH" {
		return m, nil
	}
	if i < 4 && i != 2 {
		var cmd tea.Cmd
		m.reviewers.fields[i], cmd = m.reviewers.fields[i].Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) handleReviewerPeople(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "esc" || key == "ctrl+s" {
		m.mode = modeReviewerForm
		m.input.Blur()
		m.status = ""
		return m, nil
	}
	if key == "ctrl+c" {
		m.mode = modePanel
		m.status = "Changes cancelled; nothing written."
		return m, nil
	}
	if key == "tab" || key == "shift+tab" {
		m.reviewers.searchFocus = !m.reviewers.searchFocus
		if m.reviewers.searchFocus {
			return m, m.input.Focus()
		}
		m.input.Blur()
		return m, nil
	}
	if m.reviewers.searchFocus {
		if key == "enter" {
			query := strings.TrimSpace(m.input.Value())
			if query == "" {
				m.status = "Type a name to search, or Tab to manage selected people."
				return m, nil
			}
			m.reviewers.searchID++
			id := m.reviewers.searchID
			m.status = "Searching…"
			return m, func() tea.Msg { users, err := m.svc.SearchUsers(query, 25); return reviewerSearchMsg{id, users, err} }
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	switch key {
	case "enter":
		m.mode = modeReviewerForm
		m.status = ""
		return m, nil
	case "up", "k":
		m.reviewers.result = max(0, m.reviewers.result-1)
	case "down", "j":
		m.reviewers.result = min(max(0, len(m.reviewers.results)-1), m.reviewers.result+1)
	case " ":
		if len(m.reviewers.results) == 0 {
			return m, nil
		}
		u := m.reviewers.results[m.reviewers.result]
		for i, p := range m.reviewers.people {
			if (u.ID > 0 && p.ID == u.ID) || (u.Username != "" && p.Name == u.Username) {
				m.reviewers.people = append(m.reviewers.people[:i:i], m.reviewers.people[i+1:]...)
				return m, nil
			}
		}
		if u.ID <= 0 {
			m.status = "Directory result has no numeric ID; cannot add this user."
			return m, nil
		}
		m.reviewers.people = append(m.reviewers.people, reviewerPerson{ID: u.ID, Name: u.Username, DisplayName: u.DisplayName})
	}
	return m, nil
}

func (m model) renderReviewerEditor() string {
	w := max(20, m.width-6)
	if m.mode == modeReviewerPeople {
		input := m.input
		input.Width = w - 4
		rows := []string{"Choose reviewers", "Selected: " + strutil.Truncate(peopleLabel(m.reviewers.people), w-10), "", focusRow("Search directory", w, m.reviewers.searchFocus), input.View(), ""}
		if len(m.reviewers.results) == 0 {
			rows = append(rows, "Search for people above. Selections survive new searches.")
		}
		visible := max(1, m.height-12)
		start := max(0, m.reviewers.result-visible+1)
		for i := start; i < min(len(m.reviewers.results), start+visible); i++ {
			u := m.reviewers.results[i]
			check := "[ ] "
			for _, p := range m.reviewers.people {
				if (u.ID > 0 && p.ID == u.ID) || (u.Username != "" && p.Name == u.Username) {
					check = "[x] "
				}
			}
			prefix := "  "
			if i == m.reviewers.result {
				prefix = "▸ "
			}
			rows = append(rows, focusRow(prefix+check+strutil.Truncate(u.DisplayName+" (@"+u.Username+")", w-6), w, i == m.reviewers.result && !m.reviewers.searchFocus))
		}
		return strings.Join(rows, "\n")
	}
	labels := []string{"Source branch", "Target branch", "Reviewers", "Required approvals"}
	heading := "Edit reviewer rule"
	if m.edit.action == "create" {
		heading = "New reviewer rule"
	}
	rows := []string{heading, "Empty branch = any branch. Review changes before saving.", ""}
	for i, label := range labels {
		input := m.reviewers.fields[i]
		input.Width = max(10, w-24)
		value := input.View()
		if i == 2 {
			value = strutil.Truncate(peopleLabel(m.reviewers.people), w-21) + " · Enter to choose"
		}
		if i < 2 && m.reviewers.matchers[i].Type.ID != "ANY_REF" && m.reviewers.matchers[i].Type.ID != "BRANCH" {
			value = strutil.Truncate(matcherLabel(m.reviewers.matchers[i]), w-32) + " · preserved (advanced matcher)"
		}
		prefix := "  "
		if m.reviewers.field == i {
			prefix = "▸ "
		}
		rows = append(rows, focusRow(prefix+label, w, m.reviewers.field == i)+"\n  "+value)
		if m.height >= 24 {
			rows = append(rows, "")
		}
	}
	prefix := "  "
	if m.reviewers.field == 4 {
		prefix = "▸ "
	}
	rows = append(rows, focusRow(prefix+"Review changes →", w, m.reviewers.field == 4))
	return strings.Join(rows, "\n")
}

func (m model) reviewerConfirmation() string {
	if m.edit.action == "delete" {
		var r reviewerRule
		_ = json.Unmarshal(m.edit.patch, &r)
		return fmt.Sprintf("Delete reviewer rule?\n\n%s → %s\n%s\n\nThis removes the rule from this repository.", matcherLabel(r.Source), matcherLabel(r.Target), peopleLabel(r.Reviewers))
	}
	return fmt.Sprintf("Save reviewer rule?\n\nSource: %s\nTarget: %s\nReviewers: %s\nRequired approvals: %s\n\nOther rule settings remain unchanged.", m.reviewerBranchLabel(0), m.reviewerBranchLabel(1), peopleLabel(m.reviewers.people), m.reviewers.fields[3].Value())
}

func (m model) reviewerBranchLabel(i int) string {
	if m.reviewers.matchers[i].Type.ID != "ANY_REF" && m.reviewers.matchers[i].Type.ID != "BRANCH" {
		return matcherLabel(m.reviewers.matchers[i])
	}
	if value := strings.TrimSpace(m.reviewers.fields[i].Value()); value != "" {
		return value
	}
	return "Any branch"
}
