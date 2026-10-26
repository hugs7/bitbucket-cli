package reposettings

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hugs7/bitbucket-cli/internal/api"
)

const reviewerFixture = `{"id":17,"scope":{"type":"REPOSITORY"},"sourceRefMatcher":{"id":"any","type":{"id":"ANY_REF"}},"targetRefMatcher":{"id":"refs/heads/release/2026","type":{"id":"BRANCH"}},"reviewers":[{"id":71,"name":"alice","displayName":"Alice Chen"}],"requiredApprovals":1,"reviewerGroups":[{"id":9}],"future":true}`

func loadReviewerFixture(t *testing.T, m model, raw string) model {
	t.Helper()
	m.loading = 1
	next, _ := m.Update(settingsLoadedMsg{&api.RepoSettings{Panels: map[string]api.RepoSettingsPanel{
		"Default reviewers": {Raw: json.RawMessage(raw)},
	}}})
	return next.(model)
}

func reviewerKey(m model, key tea.KeyType) model {
	next, _ := m.Update(tea.KeyMsg{Type: key})
	return next.(model)
}

func TestReviewerRuleFormEditsSelectedRuleWithoutJSON(t *testing.T) {
	for _, confirm := range []string{"y", "esc"} {
		t.Run(confirm, func(t *testing.T) {
			writes := 0
			m := settingsModel(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					if strings.HasSuffix(r.URL.Path, "/users") {
						if r.URL.Query().Get("filter") != "sam" {
							t.Error("missing search term")
						}
						fmt.Fprint(w, `{"values":[{"id":82,"name":"sam","displayName":"Sam Patel","active":true},{"id":99,"name":"disabled","active":false}]}`)
					} else {
						fmt.Fprint(w, `[`+reviewerFixture+`]`)
					}
					return
				}
				writes++
				if r.Method != "PUT" || !strings.HasSuffix(r.URL.Path, "/condition/17") {
					t.Fatalf("wrong rule: %s %s", r.Method, r.URL)
				}
				var payload struct {
					Source   reviewerMatcher    `json:"sourceMatcher"`
					Target   reviewerMatcher    `json:"targetMatcher"`
					People   []reviewerPerson   `json:"reviewers"`
					Required int                `json:"requiredApprovals"`
					Groups   []struct{ ID int } `json:"reviewerGroups"`
					Future   bool               `json:"future"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if payload.Source.Type.ID != "ANY_REF" || payload.Target.ID != "refs/heads/release/2026" || payload.Required != 2 || !payload.Future || len(payload.Groups) != 1 || payload.Groups[0].ID != 9 || len(payload.People) != 2 || payload.People[0].ID != 71 || payload.People[1].ID != 82 {
					t.Fatalf("incorrect or destructive update: %+v", payload)
				}
				w.WriteHeader(204)
			}, "Default reviewers")
			// A different first rule catches accidentally editing the first item.
			first := strings.Replace(reviewerFixture, `"id":17`, `"id":5`, 1)
			m = loadReviewerFixture(t, m, `[`+first+`,`+reviewerFixture+`]`)
			m = reviewerKey(m, tea.KeyDown)
			m, read := settingKey(m, "enter")
			if read == nil {
				t.Fatal("Enter should load the selected rule")
			}
			next, _ := m.Update(read())
			m = next.(model)
			if !strings.Contains(m.View(), "Alice Chen") || !strings.Contains(m.View(), "release/2026") || strings.Contains(m.View(), "sourceRefMatcher") {
				t.Fatal("expected a prefilled friendly form")
			}
			m = reviewerKey(m, tea.KeyTab)
			m = reviewerKey(m, tea.KeyTab)
			m, _ = settingKey(m, "enter")
			m, _ = settingKey(m, "sam")
			m, search := settingKey(m, "enter")
			next, _ = m.Update(search())
			m = next.(model)
			if !strings.Contains(m.View(), "Sam Patel") || strings.Contains(m.View(), "@disabled") {
				t.Fatal("active directory users should be selectable")
			}
			m = reviewerKey(m, tea.KeySpace)
			m, _ = settingKey(m, "enter")
			m = reviewerKey(m, tea.KeyTab)
			m = reviewerKey(m, tea.KeyCtrlU)
			m, _ = settingKey(m, "2")
			m, cmd := settingKey(m, "ctrl+s")
			if cmd != nil || writes != 0 || !strings.Contains(m.View(), "Save reviewer rule?") || !strings.Contains(m.View(), "Sam Patel") {
				t.Fatal("must review before write")
			}
			m, _ = settingKey(m, "enter")
			if writes != 0 {
				t.Fatal("Enter must not save")
			}
			m, cmd = settingKey(m, confirm)
			if confirm == "y" {
				if result := cmd().(actionDoneMsg); result.err != nil || writes != 1 {
					t.Fatalf("save failed: %+v", result)
				}
			} else if cmd != nil || writes != 0 {
				t.Fatal("cancel wrote settings")
			}
		})
	}
}

func TestReviewerRuleCreateDeleteAndInheritedControls(t *testing.T) {
	writes := 0
	m := settingsModel(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			fmt.Fprint(w, `[`+reviewerFixture+`]`)
			return
		}
		writes++
		if r.Method == "POST" {
			var raw map[string]json.RawMessage
			_ = json.NewDecoder(r.Body).Decode(&raw)
			if !strings.Contains(string(raw["sourceMatcher"]), "ANY_REF") || !strings.Contains(string(raw["targetMatcher"]), "refs/heads/main") || string(raw["reviewers"]) != "[]" || string(raw["requiredApprovals"]) != "0" {
				t.Fatalf("bad create: %s", raw)
			}
		} else if r.Method != "DELETE" || !strings.HasSuffix(r.URL.Path, "/condition/17") {
			t.Fatal("wrong deletion")
		}
		w.WriteHeader(204)
	}, "Default reviewers")
	m = loadReviewerFixture(t, m, `[`+reviewerFixture+`]`)
	m, _ = settingKey(m, "a")
	m = reviewerKey(m, tea.KeyTab)
	m, _ = settingKey(m, "main")
	m, _ = settingKey(m, "ctrl+s")
	m, cmd := settingKey(m, "y")
	if result := cmd().(actionDoneMsg); result.err != nil {
		t.Fatal(result.err)
	}
	m.mode = modePanel
	m, read := settingKey(m, "d")
	next, _ := m.Update(read())
	m = next.(model)
	if !strings.Contains(m.View(), "Delete reviewer rule?") || !strings.Contains(m.View(), "Alice Chen") {
		t.Fatal("delete must identify selected rule")
	}
	m, cmd = settingKey(m, "y")
	if result := cmd().(actionDoneMsg); result.err != nil || writes != 2 {
		t.Fatalf("delete failed: %+v", result)
	}
	m.mode = modePanel
	m = loadReviewerFixture(t, m, `[`+strings.Replace(reviewerFixture, "REPOSITORY", "PROJECT", 1)+`]`)
	for _, key := range []string{"e", "d", "J"} {
		m, cmd = settingKey(m, key)
		if cmd != nil || !strings.Contains(m.status, "Inherited rule") || writes != 2 {
			t.Fatal("inherited rule should be read-only")
		}
	}
}

func TestReviewerFormPreservesAdvancedMatchersAndFitsSmallTerminal(t *testing.T) {
	raw := strings.Replace(reviewerFixture, `"ANY_REF"`, `"PATTERN"`, 1)
	m := settingsModel(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `[`+raw+`]`) }, "Default reviewers")
	m = loadReviewerFixture(t, m, `[`+raw+`]`)
	m, cmd := settingKey(m, "e")
	next, _ := m.Update(cmd())
	m = next.(model)
	m.reviewers.fields[3].SetValue("-1")
	m, _ = settingKey(m, "ctrl+s")
	if !strings.Contains(m.status, "non-negative") {
		t.Fatal("invalid count accepted")
	}
	m.reviewers.fields[3].SetValue("2")
	m, _ = settingKey(m, "ctrl+s")
	if string(m.edit.patch) != `{"requiredApprovals":2}` {
		t.Fatalf("untouched advanced fields must be preserved: %s", m.edit.patch)
	}
	m, _ = settingKey(m, "n")
	for _, width := range []int{80, 120} {
		next, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		m = next.(model)
		if lipgloss.Width(m.View()) > width || lipgloss.Height(m.View()) > 24 {
			t.Fatalf("form overflow %dx24: %s", width, m.View())
		}
	}
}
