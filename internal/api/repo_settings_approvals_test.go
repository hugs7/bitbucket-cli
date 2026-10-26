package api_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/hugs7/bitbucket-cli/internal/api"
)

const westpacApprovals = `{"requiredApprovers":0,"com.atlassian.bitbucket.server.bitbucket-bundled-hooks:requiredApprovers":{"enable":false,"count":1}}`

func TestMinimumApprovalsAdaptsWestpacResponseWithoutPluginWrites(t *testing.T) {
	for _, count := range []int{0, 2} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			writes := 0
			svc := settingsService(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					fmt.Fprint(w, westpacApprovals)
					return
				}
				writes++
				if r.Method != "POST" || r.URL.Path != "/bitbucket/rest/api/1.0/projects/PRJ/repos/repo/settings/pull-requests" {
					t.Errorf("unexpected write %s %s", r.Method, r.URL)
				}
				body, _ := io.ReadAll(r.Body)
				assertSettingsJSON(t, body, fmt.Sprintf(`{"requiredApprovers":%d}`, count))
				w.WriteHeader(http.StatusNoContent)
			})
			current, err := api.ReadRepoSetting(svc, "PRJ", "repo", "pull-requests", "")
			if err != nil {
				t.Fatal(err)
			}
			effective, err := api.RepoMinimumApprovals(current)
			if err != nil || effective != 0 {
				t.Fatalf("plugin count must not override scalar: count=%d err=%v", effective, err)
			}
			patch, err := api.RepoMinimumApprovalsPatch(current, count)
			if err != nil {
				t.Fatal(err)
			}
			if err := api.ChangeRepoSetting(svc, "PRJ", "repo", "pull-requests", "", "update", patch); err != nil {
				t.Fatal(err)
			}
			if writes != 1 {
				t.Fatalf("writes=%d", writes)
			}
		})
	}
}

func TestMinimumApprovalsModernOnlyAndUnknownSchemas(t *testing.T) {
	current := json.RawMessage(fmt.Sprintf(`{"%s":{"enabled":true,"count":3}}`, api.RequiredApproversSetting))
	patch, err := api.RepoMinimumApprovalsPatch(current, 0)
	if err != nil {
		t.Fatal(err)
	}
	assertSettingsJSON(t, patch, fmt.Sprintf(`{"%s":{"enabled":false,"count":3}}`, api.RequiredApproversSetting))
	patch, err = api.RepoMinimumApprovalsPatch(current, 2)
	if err != nil {
		t.Fatal(err)
	}
	assertSettingsJSON(t, patch, fmt.Sprintf(`{"%s":{"enabled":true,"count":2}}`, api.RequiredApproversSetting))
	for _, raw := range []string{`{}`, `null`, `{"requiredApprovers":null}`, `{"requiredApprovers":-1}`, `{"requiredApprovers":"2"}`} {
		if _, err := api.RepoMinimumApprovalsPatch(json.RawMessage(raw), 2); err == nil {
			t.Errorf("unrecognized schema accepted: %s", raw)
		}
	}
}

func TestMinimumApprovalsRejectsIncompletePluginSchemas(t *testing.T) {
	for _, raw := range []string{
		fmt.Sprintf(`{"%s":{"enabled":true}}`, api.RequiredApproversSetting),
		fmt.Sprintf(`{"%s":{"enabled":true,"count":null}}`, api.RequiredApproversSetting),
		fmt.Sprintf(`{"%s":{"enable":false,"count":1},"%s":{"enabled":true,"count":2}}`, api.WestpacRequiredApproversSetting, api.RequiredApproversSetting),
	} {
		if _, err := api.RepoMinimumApprovalsPatch(json.RawMessage(raw), 2); err == nil {
			t.Errorf("incomplete schema accepted: %s", raw)
		}
	}
}

func TestWestpacRejectsPluginObjectMutation(t *testing.T) {
	writes := 0
	svc := settingsService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes++
		}
		fmt.Fprint(w, westpacApprovals)
	})
	for _, key := range []string{api.WestpacRequiredApproversSetting, api.RequiredApproversSetting} {
		patch := json.RawMessage(fmt.Sprintf(`{"%s":{"count":0}}`, key))
		if err := api.ChangeRepoSetting(svc, "PRJ", "repo", "pull-requests", "", "update", patch); err == nil {
			t.Fatal("plugin-object update accepted")
		}
	}
	if writes != 0 {
		t.Fatalf("unsafe plugin writes=%d", writes)
	}
}
