package reposettings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hugs7/bitbucket-cli/internal/api"
	"github.com/hugs7/bitbucket-cli/internal/tui/theme"
)

const (
	modeSettingID mode = modeConfirmDelete + 1 + iota
	modeSettingLoading
	modeSettingJSON
	modeSettingMinimum
	modeSettingConfirm
	modeSettingSaving
	modeConfirmAddWebhook
	modeReviewerForm
	modeReviewerPeople
	modeStrategyForm
)

type settingsEditState struct {
	resource, id, action string
	json                 textarea.Model
	patch                json.RawMessage
	original             json.RawMessage
	minimum              bool
	reviewerForm         bool
	strategyForm         bool
}

type settingReadMsg struct {
	raw json.RawMessage
	err error
}

func settingResource(title string) string {
	switch title {
	case "Repository details":
		return "details"
	case "Default branch":
		return "default-branch"
	case "Repository permissions":
		return "permissions-users"
	case "Group permissions":
		return "permissions-groups"
	case "Branch permissions":
		return "branch-restrictions"
	case "Access keys":
		return "access-keys"
	case "Hooks":
		return "hook-settings"
	case "Merge checks":
		return "pull-requests"
	case "Merge strategies":
		return "merge-strategies"
	case "Default reviewers":
		return "default-reviewers"
	case "Auto-decline":
		return "auto-decline"
	}
	return ""
}

func (m model) editable(p panel) bool {
	return p.editable || (api.CanEditRepoSettings(m.svc) && settingResource(p.title) != "")
}

func (m model) beginSettingEdit(action string) (tea.Model, tea.Cmd) {
	if m.loading > 0 {
		m.status = "Wait for settings to finish loading."
		return m, nil
	}
	m.edit = settingsEditState{resource: settingResource(m.activePanel().title), action: action}
	m.content.GotoTop()
	if !api.CanEditRepoSettings(m.svc) || m.edit.resource == "" {
		m.status = "Read-only or unsupported panel; use the Bitbucket web UI."
		return m, nil
	}
	if m.activePanel().title == "Hooks" && action != "update" {
		m.edit.resource = "hooks"
		if action == "create" {
			action = "enable"
		} else if action == "delete" {
			action = "disable"
		}
		m.edit.action = action
	}
	if !slices.Contains(api.RepoSettingActions(m.edit.resource), action) {
		m.status = "Supported operations: " + strings.Join(api.RepoSettingActions(m.edit.resource), ", ") + ". No implicit delete/recreate."
		return m, nil
	}
	if api.RepoSettingNeedsID(m.edit.resource, action) || strings.HasPrefix(m.edit.resource, "permissions-") {
		m.mode = modeSettingID
		m.input.SetValue("")
		m.input.Placeholder = "ID, username, group name or plugin:hook key"
		m.input.Focus()
		m.status = action + " " + m.edit.resource + ": enter ID/name shown in panel"
		if m.edit.resource == "hook-settings" {
			m.status += ". Explicit config editing may reveal plugin secrets."
		}
		return m, textinput.Blink
	}
	if action == "create" {
		return m.openSettingJSON(settingCreateTemplate(m.edit.resource))
	}
	if action == "delete" {
		m.mode = modeSettingConfirm
		return m, nil
	}
	return m.loadSetting()
}

func (m model) loadSetting() (tea.Model, tea.Cmd) {
	m.mode = modeSettingLoading
	m.content.GotoTop()
	m.status = "Loading complete settings JSON…"
	return m, func() tea.Msg {
		raw, err := api.ReadRepoSetting(m.svc, m.project, m.slug, m.edit.resource, m.edit.id)
		return settingReadMsg{raw: raw, err: err}
	}
}

func (m model) openSettingJSON(raw json.RawMessage) (tea.Model, tea.Cmd) {
	if m.edit.resource == "access-keys" && m.edit.action == "update" {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err == nil {
			raw, _ = json.Marshal(map[string]json.RawMessage{"permission": fields["permission"]})
		}
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, raw, "", "  "); err != nil {
		m.mode = modePanel
		m.status = "Invalid settings JSON: " + err.Error()
		return m, nil
	}
	m.edit.json = textarea.New()
	m.edit.json.CharLimit = 0
	m.edit.json.SetWidth(m.contentWidth() - 4)
	m.edit.json.SetHeight(max(5, m.bodyHeight()-7))
	m.edit.json.SetValue(pretty.String())
	m.edit.original = raw
	m.mode = modeSettingJSON
	m.status = "Edit JSON; ctrl+s reviews the write; esc cancels. Omitted fields are preserved."
	return m, m.edit.json.Focus()
}

func (m model) handleSettingKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeSettingLoading || m.mode == modeSettingSaving {
		// Do not allow a second operation or leave while a write is in flight.
		return m, nil
	}
	if msg.String() == "esc" || msg.String() == "ctrl+c" {
		m.mode = modePanel
		m.status = "Settings change cancelled; nothing written."
		m.input.Blur()
		return m, nil
	}
	switch m.mode {
	case modeConfirmAddWebhook:
		if msg.String() == "y" || msg.String() == "Y" {
			m.mode = modeSettingSaving
			return m, m.addWebhook()
		}
		if msg.String() == "n" || msg.String() == "N" {
			m.mode = modePanel
			m.clearAddState()
			m.status = "Add webhook cancelled."
		}
	case modeSettingConfirm:
		if msg.String() == "y" || msg.String() == "Y" {
			m.mode = modeSettingSaving
			m.status = "Saving settings…"
			return m, func() tea.Msg {
				err := api.ChangeRepoSetting(m.svc, m.project, m.slug, m.edit.resource, m.edit.id, m.edit.action, m.edit.patch)
				return actionDoneMsg{text: m.edit.action + " " + m.edit.resource, err: err}
			}
		}
		if msg.String() == "n" || msg.String() == "N" {
			if m.edit.reviewerForm && m.edit.action != "delete" {
				m.mode = modeReviewerForm
				return m, nil
			}
			if m.edit.strategyForm {
				m.mode = modeStrategyForm
				return m, nil
			}
			m.mode = modePanel
			m.status = "Settings change cancelled; nothing written."
			return m, nil
		}
		m.content.Height = m.bodyHeight()
		m.content.SetContent(lipgloss.NewStyle().Width(m.contentWidth() - 2).Render(m.renderSettingEdit()))
		var cmd tea.Cmd
		m.content, cmd = m.content.Update(msg)
		return m, cmd
	case modeSettingJSON:
		if msg.String() == "ctrl+s" {
			var object map[string]json.RawMessage
			if err := json.Unmarshal([]byte(m.edit.json.Value()), &object); err != nil || object == nil {
				m.status = "Invalid JSON: settings must be an object. Fix it or esc to cancel."
				return m, nil
			}
			if m.edit.action == "update" && !(m.edit.resource == "merge-strategies" && len(object) == 0) {
				object = changedSettingFields(m.edit.original, object)
				if len(object) == 0 {
					m.status = "No changed fields; nothing written. Omitted fields are preserved."
					return m, nil
				}
			}
			m.edit.patch, _ = json.MarshalIndent(object, "", "  ")
			m.mode = modeSettingConfirm
			m.content.GotoTop()
			m.status = "Review this write, then press y to apply, n to cancel."
			return m, nil
		}
		var cmd tea.Cmd
		m.edit.json, cmd = m.edit.json.Update(msg)
		return m, cmd
	case modeSettingID, modeSettingMinimum:
		if msg.String() == "enter" {
			value := strings.TrimSpace(m.input.Value())
			if value == "" {
				m.status = "A value is required."
				return m, nil
			}
			if m.mode == modeSettingMinimum {
				n, err := strconv.Atoi(value)
				if err != nil || n < 0 {
					m.status = "Enter a non-negative integer."
					return m, nil
				}
				patch, err := api.RepoMinimumApprovalsPatch(m.edit.original, n)
				if err != nil {
					m.status = err.Error()
					return m, nil
				}
				m.input.Blur()
				m.edit.patch = patch
				m.mode = modeSettingConfirm
				return m, nil
			}
			m.input.Blur()
			m.edit.id = value
			if m.edit.action == "create" {
				return m.openSettingJSON(settingCreateTemplate(m.edit.resource))
			}
			return m.loadSetting()
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func settingCreateTemplate(resource string) json.RawMessage {
	switch resource {
	case "default-reviewers":
		return json.RawMessage(`{"sourceMatcher":{"id":"any","type":{"id":"ANY_REF"}},"targetMatcher":{"id":"any","type":{"id":"ANY_REF"}},"reviewers":[],"requiredApprovals":0}`)
	case "permissions-users", "permissions-groups":
		return json.RawMessage(`{"permission":"REPO_READ"}`)
	case "branch-restrictions":
		return json.RawMessage(`{"type":"no-deletes","matcher":{"id":"refs/heads/main","type":{"id":"BRANCH"}},"users":[],"groups":[],"accessKeys":[]}`)
	case "access-keys":
		return json.RawMessage(`{"key":{"text":"PASTE PUBLIC OPENSSH KEY ONLY","label":"CI"},"permission":"REPO_READ"}`)
	}
	return json.RawMessage(`{}`)
}

func changedSettingFields(original json.RawMessage, edited map[string]json.RawMessage) map[string]json.RawMessage {
	var before map[string]json.RawMessage
	_ = json.Unmarshal(original, &before)
	for key, raw := range edited {
		var old, next any
		oldDecoder := json.NewDecoder(bytes.NewReader(before[key]))
		oldDecoder.UseNumber()
		nextDecoder := json.NewDecoder(bytes.NewReader(raw))
		nextDecoder.UseNumber()
		if oldDecoder.Decode(&old) == nil && nextDecoder.Decode(&next) == nil && reflect.DeepEqual(old, next) {
			delete(edited, key)
		}
	}
	return edited
}

func (m model) renderSettingEdit() string {
	heading := theme.TitleBadge.Render(" " + strings.ToUpper(m.edit.action+" "+m.edit.resource) + " ")
	switch m.mode {
	case modeSettingID, modeSettingMinimum:
		return heading + "\n\n" + m.status + "\n\n" + m.input.View()
	case modeSettingLoading, modeSettingSaving:
		return heading + "\n\n" + m.status
	case modeSettingJSON:
		return heading + "\nctrl+s review · esc cancel\nObjects merge; arrays replace. Empty mergeConfig restores inheritance.\n" + m.edit.json.View()
	case modeSettingConfirm:
		if m.edit.reviewerForm {
			return m.reviewerConfirmation()
		}
		if m.edit.strategyForm {
			return m.strategyConfirmation()
		}
		body := fmt.Sprintf("%s\n\nApply %s %s %s to %s/%s?\nExplicit y confirms · n/esc cancels\n", heading, m.edit.action, m.edit.resource, m.edit.id, m.project, m.slug)
		if m.edit.action == "delete" && m.edit.resource == "auto-decline" {
			body += "Deletes the repository override, restoring inherited auto-decline settings.\n"
		}
		if m.edit.resource == "hook-settings" || m.edit.resource == "access-keys" {
			return body + "\nPayload hidden; review in the editor before saving."
		}
		return body + "\n" + string(m.edit.patch)
	case modeConfirmAddWebhook:
		return fmt.Sprintf("Create webhook on %s/%s?\nURL: %s\nEvents: %s\nDescription: %s\n\ny confirms · n/esc cancels", m.project, m.slug, m.addURL, m.addEvents, m.addDescription)
	}
	return ""
}
