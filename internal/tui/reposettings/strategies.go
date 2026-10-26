package reposettings

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/hugs7/bitbucket-cli/internal/api"
)

type strategyOption struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     *bool  `json:"enabled"`
}

func (s strategyOption) allowed() bool { return s.Enabled == nil || *s.Enabled }

type strategyEditor struct {
	options   []strategyOption
	defaultID string
	inherit   bool
	cursor    int // project inheritance, strategies, then Review changes
	scope     string
}

// readStrategyConfig uses the server's catalog, including disabled and plugin
// strategies. It never substitutes a guessed list when settings are unavailable.
func readStrategyConfig(raw json.RawMessage) (strategyEditor, error) {
	var config struct {
		Strategies []strategyOption `json:"strategies"`
		Default    struct {
			ID string `json:"id"`
		} `json:"defaultStrategy"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return strategyEditor{}, fmt.Errorf("Cannot read merge strategies: %w", err)
	}
	if len(config.Strategies) == 0 {
		return strategyEditor{}, fmt.Errorf("Server returned no strategy catalog. Refresh or use J for advanced settings.")
	}
	seen := map[string]bool{}
	for i := range config.Strategies {
		strategy := &config.Strategies[i]
		if strategy.ID == "" || seen[strategy.ID] {
			return strategyEditor{}, fmt.Errorf("Server returned missing or duplicate strategy IDs; editing is unavailable.")
		}
		seen[strategy.ID] = true
		if strategy.Name == "" {
			strategy.Name = strategy.ID
		}
	}
	return strategyEditor{options: config.Strategies, defaultID: config.Default.ID, inherit: config.Type == "PROJECT", scope: config.Type}, nil
}

func (m model) strategyPanel() bool {
	return m.activePanel().title == "Merge strategies" && api.CanEditRepoSettings(m.svc)
}

func (m model) openStrategyForm(raw json.RawMessage) (tea.Model, tea.Cmd) {
	config, err := readStrategyConfig(raw)
	if err != nil {
		m.mode = modePanel
		m.status = err.Error()
		return m, nil
	}
	m.strategies = config
	m.edit.original = raw
	m.mode = modeStrategyForm
	m.status = ""
	return m, nil
}

// strategyPatch sends only changed fields. Strategy objects keep their original
// metadata; the API removes disabled strategies from the POST's enabled list.
func (m model) strategyPatch() (json.RawMessage, error) {
	if m.strategies.inherit {
		if m.strategies.scope == "PROJECT" {
			return nil, fmt.Errorf("Already using project settings; nothing to save.")
		}
		return json.RawMessage(`{}`), nil
	}
	var original struct {
		Strategies []json.RawMessage `json:"strategies"`
		Default    struct {
			ID string `json:"id"`
		} `json:"defaultStrategy"`
	}
	if err := json.Unmarshal(m.edit.original, &original); err != nil {
		return nil, err
	}
	selected := -1
	changed := m.strategies.scope == "PROJECT"
	options := make([]map[string]json.RawMessage, len(original.Strategies))
	for i, raw := range original.Strategies {
		if err := json.Unmarshal(raw, &options[i]); err != nil {
			return nil, err
		}
		var before strategyOption
		if err := json.Unmarshal(raw, &before); err != nil {
			return nil, err
		}
		after := m.strategies.options[i]
		changed = changed || before.allowed() != after.allowed()
		options[i]["enabled"], _ = json.Marshal(after.allowed())
		if after.ID == m.strategies.defaultID && after.allowed() {
			selected = i
		}
	}
	if selected < 0 {
		return nil, fmt.Errorf("Choose an allowed default strategy with d before saving.")
	}
	patch := map[string]json.RawMessage{}
	if changed {
		patch["strategies"], _ = json.Marshal(options)
	}
	if original.Default.ID != m.strategies.defaultID || m.strategies.scope == "PROJECT" {
		patch["defaultStrategy"], _ = json.Marshal(options[selected])
	}
	if len(patch) == 0 {
		return nil, fmt.Errorf("No changes to save.")
	}
	return json.Marshal(patch)
}

func (m model) handleStrategyForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := &m.strategies
	last := len(s.options) + 1
	switch msg.String() {
	case "esc", "ctrl+c":
		m.mode = modePanel
		m.status = "Changes cancelled; nothing written."
		return m, nil
	case "up", "k", "shift+tab":
		s.cursor = max(0, s.cursor-1)
	case "down", "j", "tab":
		s.cursor = min(last, s.cursor+1)
	case "home":
		s.cursor = 0
	case "end":
		s.cursor = last
	case "d":
		if s.cursor > 0 && s.cursor < last {
			enabled := true
			s.options[s.cursor-1].Enabled = &enabled
			s.defaultID = s.options[s.cursor-1].ID
			s.inherit = false
			m.status = "Default selected for this repository; project settings are unchanged."
		}
	case " ", "enter":
		if s.cursor == last {
			return m.reviewStrategyChanges()
		}
		if s.cursor == 0 {
			s.inherit = !s.inherit
			if s.inherit {
				// Discard unsaved repository edits when returning to inheritance.
				original, _ := readStrategyConfig(m.edit.original)
				original.inherit = true
				*s = original
			}
			m.status = ""
			return m, nil
		}
		option := &s.options[s.cursor-1]
		if option.ID == s.defaultID && option.allowed() {
			m.status = "Select another default with d before disabling this strategy."
			return m, nil
		}
		enabled := !option.allowed()
		option.Enabled = &enabled
		s.inherit = false
		m.status = "Only this repository will change; project settings are unchanged."
	case "ctrl+s":
		return m.reviewStrategyChanges()
	}
	return m, nil
}

func (m model) reviewStrategyChanges() (tea.Model, tea.Cmd) {
	patch, err := m.strategyPatch()
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

func strategyLabel(option strategyOption, defaultID string) string {
	check := "[ ] "
	if option.allowed() {
		check = "[x] "
	}
	label := check + option.Name
	if option.ID == defaultID {
		label += "  • default"
	}
	return label
}

func (m model) renderStrategyPanel() string {
	if m.settings == nil {
		return "Loading merge strategies…"
	}
	panel := m.settings.Panels["Merge strategies"]
	if panel.Error != "" {
		return panel.Error
	}
	config, err := readStrategyConfig(panel.Raw)
	if err != nil {
		return err.Error()
	}
	scope := "Repository settings"
	if config.inherit {
		scope = "Inherited from project"
	}
	rows := []string{scope, "Enter/e to edit allowed strategies and default", ""}
	for _, option := range config.options {
		rows = append(rows, strategyLabel(option, config.defaultID))
	}
	return strings.Join(rows, "\n")
}

func (m model) renderStrategyForm() string {
	w := max(20, m.width-4)
	s := m.strategies
	scope := "Repository override"
	if s.inherit {
		scope = "Use project settings"
	}
	rows := []string{"Merge strategies", scope, "Space toggles · d chooses the default (and allows it)", ""}
	inherit := "[ ] Use project settings"
	if s.inherit {
		inherit = "[x] Use project settings"
	}
	choices := []string{inherit}
	for _, option := range s.options {
		choices = append(choices, strategyLabel(option, s.defaultID))
	}
	choices = append(choices, "Review changes →")
	// Keep focused controls visible even with a long plugin catalog or small terminal.
	visible := max(2, m.height-lipgloss.Height(m.footerHelp())-lipgloss.Height(m.footerStatus())-11)
	start := max(0, s.cursor-visible+1)
	for i := start; i < min(len(choices), start+visible); i++ {
		prefix := "  "
		if i == s.cursor {
			prefix = "▸ "
		}
		rows = append(rows, focusRow(ansi.Truncate(prefix+choices[i], w, "…"), w, i == s.cursor))
	}
	if start > 0 || start+visible < len(choices) {
		rows = append(rows, fmt.Sprintf("  %d–%d of %d · ↑/↓ more", start+1, min(len(choices), start+visible), len(choices)))
	} else {
		rows = append(rows, "")
	}
	note := "Changes apply only after review and confirmation."
	if s.cursor > 0 && s.cursor <= len(s.options) && s.options[s.cursor-1].Description != "" {
		note = s.options[s.cursor-1].Description
	}
	if s.inherit && s.scope != "PROJECT" {
		note = "Saving removes this repository's override. Project values take effect; the list shows the current repository snapshot."
	}
	rows = append(rows, "", clampLines(lipgloss.NewStyle().Width(w).Render(note), 3))
	return strings.Join(rows, "\n")
}

func (m model) strategyConfirmation() string {
	if m.strategies.inherit {
		return "Use project merge settings?\n\nRemove this repository's override.\nFuture project changes will apply automatically.\n\nThe project itself will not be changed."
	}
	rows := []string{"Save merge strategies?", "", "Allowed in this repository:"}
	for _, option := range m.strategies.options {
		if option.allowed() {
			rows = append(rows, "  "+strategyLabel(option, m.strategies.defaultID))
		}
	}
	rows = append(rows, "", "All other strategies will be unavailable for merging.", "Other repository settings and the project remain unchanged.")
	return strings.Join(rows, "\n")
}
