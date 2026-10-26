package reposettings

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const strategyFixture = `{"type":"PROJECT","commitSummaries":20,"future":true,"defaultStrategy":{"id":"no-ff","name":"Merge commit"},"strategies":[{"id":"no-ff","name":"Merge commit","enabled":true,"flag":"--no-ff"},{"id":"squash","name":"Squash","enabled":false,"description":"Combine changes into one commit.","pluginData":42},{"id":"ff-only","name":"Fast-forward only","enabled":false}]}`

func TestStrategyFormEditsAndConfirmsWithoutJSON(t *testing.T) {
	for _, confirm := range []string{"y", "esc"} {
		t.Run(confirm, func(t *testing.T) {
			writes := 0
			m := settingsModel(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					fmt.Fprint(w, `{"mergeConfig":`+strategyFixture+`}`)
					return
				}
				writes++
				if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/settings/pull-requests") {
					t.Errorf("unexpected write %s %s", r.Method, r.URL.Path)
				}
				var payload struct {
					Config struct {
						Type            string
						CommitSummaries int
						Future          bool
						Default         struct{ ID, Name string } `json:"defaultStrategy"`
						Strategies      []struct {
							ID         string
							PluginData int
						}
					} `json:"mergeConfig"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				c := payload.Config
				if c.Type != "" || c.CommitSummaries != 20 || !c.Future || c.Default.ID != "squash" || c.Default.Name != "Squash" || len(c.Strategies) != 1 || c.Strategies[0].ID != "squash" || c.Strategies[0].PluginData != 42 {
					t.Errorf("incorrect/destructive payload: %+v", c)
				}
				w.WriteHeader(http.StatusNoContent)
			}, "Merge strategies")
			if confirm == "esc" {
				m.mode = modePanels // The visible shortcut also works from the sidebar.
			}
			m, read := settingKey(m, "e")
			if read == nil {
				t.Fatal("e should load the structured editor")
			}
			next, _ := m.Update(read())
			m = next.(model)
			if !strings.Contains(m.View(), "[x] Use project settings") || !strings.Contains(m.View(), "[ ] Squash") || strings.Contains(m.View(), "commitSummaries") {
				t.Fatal("missing prefilled checklist")
			}
			m = reviewerKey(m, tea.KeyDown)
			m = reviewerKey(m, tea.KeySpace)
			if !strings.Contains(m.View(), "Select another default") {
				t.Fatal("must protect the current default")
			}
			m = reviewerKey(m, tea.KeyDown)
			m, _ = settingKey(m, "d") // Also enable previously disabled Squash.
			m = reviewerKey(m, tea.KeyUp)
			m = reviewerKey(m, tea.KeySpace) // Disable the previous default.
			m, cmd := settingKey(m, "ctrl+s")
			if cmd != nil || writes != 0 || !strings.Contains(m.View(), "Save merge strategies?") || !strings.Contains(m.View(), "Squash  • default") {
				t.Fatal("must review before writing")
			}
			m, _ = settingKey(m, "n")
			if !strings.Contains(m.View(), "[ ] Merge commit") || !strings.Contains(m.View(), "[x] Squash") {
				t.Fatal("n should return to the edited form")
			}
			m, _ = settingKey(m, "ctrl+s")
			m, cmd = settingKey(m, "enter")
			if cmd != nil || writes != 0 {
				t.Fatal("Enter must not confirm")
			}
			_, cmd = settingKey(m, confirm)
			if confirm == "y" {
				if cmd == nil {
					t.Fatal("missing save")
				}
				if result := cmd().(actionDoneMsg); result.err != nil || writes != 1 {
					t.Fatalf("save failed: %+v", result)
				}
			} else if cmd != nil || writes != 0 {
				t.Fatal("cancel wrote settings")
			}
		})
	}
}

func TestStrategyFormRestoresInheritanceAndKeepsAdvancedEditor(t *testing.T) {
	writes := 0
	m := settingsModel(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `{"mergeConfig":`+strings.Replace(strategyFixture, "PROJECT", "REPOSITORY", 1)+`}`)
			return
		}
		writes++
		var payload map[string]map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if config, ok := payload["mergeConfig"]; !ok || len(config) != 0 || len(payload) != 1 {
			t.Fatalf("must restore inheritance with empty config: %v", payload)
		}
		w.WriteHeader(http.StatusNoContent)
	}, "Merge strategies")
	m, read := settingKey(m, "enter")
	next, _ := m.Update(read())
	m = next.(model)
	m, _ = settingKey(m, "ctrl+s")
	if m.mode != modeStrategyForm || !strings.Contains(m.View(), "No changes") {
		t.Fatal("unchanged form must not accidentally restore inheritance")
	}
	m = reviewerKey(m, tea.KeySpace)
	m, _ = settingKey(m, "ctrl+s")
	if !strings.Contains(m.View(), "Remove this repository's override") || writes != 0 {
		t.Fatal("missing restore confirmation")
	}
	m, write := settingKey(m, "y")
	if result := write().(actionDoneMsg); result.err != nil || writes != 1 {
		t.Fatalf("restore failed: %+v", result)
	}
	m.mode = modePanel
	m, read = settingKey(m, "J")
	next, _ = m.Update(read())
	if next.(model).mode != modeSettingJSON {
		t.Fatal("J must retain advanced JSON")
	}
}

func TestStrategyFormHandlesUnavailableCatalogAndSmallTerminal(t *testing.T) {
	for _, fixture := range []string{`{}`, `{"strategies":[{"id":"x"},{"id":"x"}]}`, strategyFixture} {
		m := settingsModel(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"mergeConfig":`+fixture+`}`) }, "Merge strategies")
		m, read := settingKey(m, "e")
		next, _ := m.Update(read())
		m = next.(model)
		if fixture != strategyFixture {
			if m.mode == modeStrategyForm {
				t.Fatal("cannot edit an invalid catalog")
			}
			continue
		}
		m, _ = settingKey(m, "ctrl+s")
		if !strings.Contains(m.View(), "Already using project settings") {
			t.Fatal("unchanged inherited config must not save")
		}
		m = reviewerKey(m, tea.KeyDown)
		m = reviewerKey(m, tea.KeyDown)
		m, _ = settingKey(m, "d")
		m = reviewerKey(m, tea.KeyHome)
		m = reviewerKey(m, tea.KeySpace)
		if !strings.Contains(m.View(), "[x] Merge commit  • default") || !strings.Contains(m.View(), "[ ] Squash") {
			t.Fatal("returning to project settings must discard the unsaved override")
		}
		for _, width := range []int{80, 120} {
			next, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: 20})
			m = reviewerKey(next.(model), tea.KeyEnd)
			view := m.View()
			if !strings.Contains(view, "▸ Review changes") || lipgloss.Width(view) > width || lipgloss.Height(view) > 20 {
				t.Fatalf("unreachable or overflowing form at %d: %s", width, view)
			}
		}
	}
}
