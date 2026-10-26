package reposettings

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hugs7/bitbucket-cli/internal/api"
	"github.com/hugs7/bitbucket-cli/internal/config"
)

func settingsModel(t *testing.T, handler http.HandlerFunc, title string) model {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	svc, err := api.NewService("settings.test", config.Host{Type: "server", APIBase: server.URL + "/rest/api/1.0"})
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(svc, "PRJ", "repo")
	m.loading = 0
	m.mode = modePanel
	for i, p := range panels {
		if p.title == title {
			m.panelIdx = i
		}
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	return next.(model)
}

func settingKey(m model, text string) (model, tea.Cmd) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)}
	switch text {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+s":
		msg = tea.KeyMsg{Type: tea.KeyCtrlS}
	}
	next, cmd := m.Update(msg)
	return next.(model), cmd
}

func TestSettingsTUIMinimumApprovalsRequiresExplicitConfirmation(t *testing.T) {
	for _, confirm := range []string{"n", "esc", "y"} {
		t.Run(confirm, func(t *testing.T) {
			writes := 0
			m := settingsModel(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					fmt.Fprint(w, `{"requiredApprovers":1,"mergeConfig":{"type":"PROJECT"}}`)
					return
				}
				writes++
				body, _ := io.ReadAll(r.Body)
				if string(body) != `{"requiredApprovers":3}` {
					t.Errorf("unexpected payload %s", body)
				}
				w.WriteHeader(http.StatusNoContent)
			}, "Merge checks")
			m, read := settingKey(m, "m")
			next, _ := m.Update(read())
			m = next.(model)
			if m.mode != modeSettingMinimum {
				t.Fatalf("mode = %v", m.mode)
			}
			m.input.SetValue("3")
			m, cmd := settingKey(m, "enter")
			if cmd != nil || m.mode != modeSettingConfirm || writes != 0 {
				t.Fatal("must review before writing")
			}
			if !strings.Contains(m.View(), "Apply update pull-requests") {
				t.Fatal("confirmation not rendered")
			}
			m, cmd = settingKey(m, "enter")
			if cmd != nil || writes != 0 || m.mode != modeSettingConfirm {
				t.Fatal("Enter must not confirm a write")
			}
			m, cmd = settingKey(m, confirm)
			if confirm == "y" {
				if cmd == nil || m.mode != modeSettingSaving {
					t.Fatal("explicit y should schedule a write")
				}
				result := cmd().(actionDoneMsg)
				if result.err != nil || writes != 1 {
					t.Fatalf("writes=%d err=%v", writes, result.err)
				}
			} else if cmd != nil || writes != 0 || m.mode != modePanel {
				t.Fatal("cancel must not write")
			}
		})
	}
}

func TestSettingsTUIJSONEditorOnlyWritesChangedFragments(t *testing.T) {
	writes := 0
	m := settingsModel(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			fmt.Fprint(w, `{"requiredAllTasksComplete":false,"mergeConfig":{"type":"PROJECT"},"future":123}`)
			return
		}
		writes++
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if len(body) != 1 || string(body["requiredAllTasksComplete"]) != "true" {
			t.Errorf("unexpected payload %+v", body)
		}
		w.WriteHeader(http.StatusNoContent)
	}, "Merge checks")
	m, cmd := settingKey(m, "e")
	if cmd == nil {
		t.Fatal("expected read command")
	}
	next, _ := m.Update(cmd())
	m = next.(model)
	if m.mode != modeSettingJSON {
		t.Fatalf("expected editor: %s", m.View())
	}
	m, cmd = settingKey(m, "ctrl+s")
	if cmd != nil || m.mode != modeSettingJSON || !strings.Contains(m.status, "No changed") {
		t.Fatal("unchanged document should not write")
	}
	m.edit.json.SetValue(`not json`)
	m, cmd = settingKey(m, "ctrl+s")
	if cmd != nil || m.mode != modeSettingJSON || !strings.Contains(m.status, "Invalid JSON") {
		t.Fatal("invalid JSON should remain editable")
	}
	m.edit.json.SetValue(`{"requiredAllTasksComplete":true,"mergeConfig":{"type":"PROJECT"},"future":123}`)
	m, cmd = settingKey(m, "ctrl+s")
	if cmd != nil || m.mode != modeSettingConfirm || writes != 0 {
		t.Fatal("JSON save must first confirm")
	}
	m, cmd = settingKey(m, "y")
	result := cmd().(actionDoneMsg)
	if result.err != nil || writes != 1 {
		t.Fatalf("writes=%d err=%v", writes, result.err)
	}
}

func TestSettingsTUIReportsReadErrorsAndKeepsSelectedPanelVisible(t *testing.T) {
	m := settingsModel(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("unexpected write")
		}
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"errors":[{"message":"admin access denied"}]}`)
	}, "Auto-decline")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 18})
	m = next.(model)
	if !strings.Contains(m.renderPanels(), "Auto-decline") {
		t.Fatal("selected panel must remain visible on a small terminal")
	}
	m, cmd := settingKey(m, "e")
	next, _ = m.Update(cmd())
	m = next.(model)
	if m.mode != modePanel || !strings.Contains(strings.Join(strings.Fields(m.View()), " "), "admin access denied") {
		t.Fatalf("error not visible: %s", m.View())
	}
}
