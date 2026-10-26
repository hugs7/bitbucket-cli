package reposettings

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestTUIWestpacMinimumUsesScalarNotDisabledPluginCount(t *testing.T) {
	for _, desired := range []int{0, 2} {
		t.Run(fmt.Sprint(desired), func(t *testing.T) {
			writes := 0
			m := settingsModel(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					fmt.Fprint(w, `{"requiredApprovers":0,"com.atlassian.bitbucket.server.bitbucket-bundled-hooks:requiredApprovers":{"enable":false,"count":1}}`)
					return
				}
				writes++
				body, _ := io.ReadAll(r.Body)
				if string(body) != fmt.Sprintf(`{"requiredApprovers":%d}`, desired) {
					t.Errorf("unsafe Westpac payload: %s", body)
				}
				w.WriteHeader(http.StatusNoContent)
			}, "Merge checks")
			m, read := settingKey(m, "m")
			next, _ := m.Update(read())
			m = next.(model)
			if m.input.Value() != "0" || !strings.Contains(m.View(), "Current minimum approving reviewers: 0") {
				t.Fatalf("incorrect current minimum: %s", m.View())
			}
			m.input.SetValue(fmt.Sprint(desired))
			m, cmd := settingKey(m, "enter")
			if cmd != nil || writes != 0 || m.mode != modeSettingConfirm {
				t.Fatal("minimum change must be confirmed")
			}
			m, cmd = settingKey(m, "y")
			if result := cmd().(actionDoneMsg); result.err != nil || writes != 1 {
				t.Fatalf("writes=%d error=%v", writes, result.err)
			}
		})
	}
}

func TestTUIHookEnableUsesTextKeyAndDoesNotReadConfig(t *testing.T) {
	writes := 0
	m := settingsModel(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/settings") {
			t.Error("enabling should not fetch configuration or secrets")
		}
		if r.Method == "GET" {
			fmt.Fprint(w, `{"details":{"key":"example:hook","name":"Hook"},"enabled":false}`)
			return
		}
		writes++
		body, _ := io.ReadAll(r.Body)
		if r.Method != "PUT" || !strings.HasSuffix(r.URL.Path, "/example:hook/enabled") || len(body) != 0 {
			t.Errorf("unexpected hook request %s %s body=%s", r.Method, r.URL, body)
		}
		w.WriteHeader(http.StatusNoContent)
	}, "Hooks")
	m, _ = settingKey(m, "a")
	m.input.SetValue("example:hook")
	m, read := settingKey(m, "enter")
	next, _ := m.Update(read())
	m = next.(model)
	if m.mode != modeSettingConfirm || writes != 0 {
		t.Fatal("hook enable must be confirmed")
	}
	m, write := settingKey(m, "y")
	if result := write().(actionDoneMsg); result.err != nil || writes != 1 {
		t.Fatalf("writes=%d error=%v", writes, result.err)
	}
}

func TestTUIGroupGrantAcceptsGroupNameAndBranchUpdateIsNotInvented(t *testing.T) {
	m := settingsModel(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no HTTP request expected yet: %s", r.URL)
	}, "Group permissions")
	m, _ = settingKey(m, "a")
	m.input.SetValue("Dev Team")
	m, _ = settingKey(m, "enter")
	if m.mode != modeSettingJSON || m.edit.id != "Dev Team" || !strings.Contains(m.edit.json.Value(), "REPO_READ") {
		t.Fatalf("grant editor failed: %s", m.View())
	}
	m.mode = modePanel
	for i, p := range panels {
		if p.title == "Branch permissions" {
			m.panelIdx = i
		}
	}
	m, cmd := settingKey(m, "e")
	if cmd != nil || !strings.Contains(m.status, "No implicit delete/recreate") {
		t.Fatal("branch restriction update must not invent PUT or silently recreate")
	}
}
