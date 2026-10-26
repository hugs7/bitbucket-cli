package api_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/hugs7/bitbucket-cli/internal/api"
	"github.com/hugs7/bitbucket-cli/internal/config"
)

func settingsService(t *testing.T, handler http.HandlerFunc) api.Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	svc, err := api.NewService("test.invalid", config.Host{Type: "server", APIBase: server.URL + "/bitbucket/rest/api/1.0", Token: "test-token"})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func assertSettingsJSON(t *testing.T, actual []byte, expected string) {
	t.Helper()
	var a, b any
	if err := json.Unmarshal(actual, &a); err != nil {
		t.Fatalf("invalid JSON %q: %v", actual, err)
	}
	if err := json.Unmarshal([]byte(expected), &b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("JSON got %s; want %s", actual, expected)
	}
}

func TestRepoSettingWritesDocumentedMethodsAndPreservesFields(t *testing.T) {
	const prPath = "/bitbucket/rest/api/1.0/projects/PRJ/repos/repo/settings/pull-requests"
	const conditionPath = "/bitbucket/rest/default-reviewers/latest/projects/PRJ/repos/repo"
	condition := `{"id":4,"scope":{"type":"REPOSITORY"},"sourceRefMatcher":{"id":"any","type":{"id":"ANY_REF"}},"targetRefMatcher":{"id":"refs/heads/main","type":{"id":"BRANCH"}},"reviewers":[{"id":123}],"requiredApprovals":1,"reviewerGroups":[{"id":12}]}`
	tests := []struct {
		name, resource, id, action, current, patch, method, path, expected string
	}{
		{"minimum approvals leaves other fragments untouched", "pull-requests", "", "update",
			fmt.Sprintf(`{"mergeConfig":{"type":"PROJECT"},"requiredApprovers":1,"requiredAllTasksComplete":true,"%s":{"enabled":true,"count":1,"future":"keep"}}`, api.RequiredApproversSetting),
			fmt.Sprintf(`{"%s":{"count":3}}`, api.RequiredApproversSetting), "POST", prPath,
			fmt.Sprintf(`{"%s":{"enabled":true,"count":3,"future":"keep"}}`, api.RequiredApproversSetting)},
		{"all tasks and legacy minimum", "pull-requests", "", "update", `{"requiredAllTasksComplete":false,"requiredApprovers":1}`, `{"requiredAllTasksComplete":true,"requiredApprovers":2}`, "POST", prPath, `{"requiredAllTasksComplete":true,"requiredApprovers":2}`},
		{"merge default preserves enabled list and future fields", "merge-strategies", "", "update",
			`{"requiredAllApprovers":true,"mergeConfig":{"type":"PROJECT","commitSummaries":20,"future":true,"defaultStrategy":{"id":"no-ff"},"strategies":[{"id":"no-ff","enabled":true},{"id":"squash","enabled":true},{"id":"ff","enabled":false}]}}`,
			`{"defaultStrategy":{"id":"squash"}}`, "POST", prPath,
			`{"mergeConfig":{"commitSummaries":20,"future":true,"defaultStrategy":{"id":"squash"},"strategies":[{"id":"no-ff","enabled":true},{"id":"squash","enabled":true}]}}`},
		{"merge inheritance", "merge-strategies", "", "update", `{"mergeConfig":{"strategies":[{"id":"no-ff"}]}}`, `{}`, "POST", prPath, `{"mergeConfig":{}}`},
		{"PR merge inheritance", "pull-requests", "", "update", `{"mergeConfig":{"strategies":[{"id":"no-ff"}]},"requiredApprovers":2}`, `{"mergeConfig":{}}`, "POST", prPath, `{"mergeConfig":{}}`},
		{"auto decline", "auto-decline", "", "update", `{"enabled":true,"inactivityWeeks":4,"future":42,"scope":{"type":"PROJECT"}}`, `{"inactivityWeeks":8}`, "PUT", "/bitbucket/rest/api/1.0/projects/PRJ/repos/repo/settings/auto-decline", `{"enabled":true,"inactivityWeeks":8,"future":42}`},
		{"condition minimum preserves matchers reviewers groups", "default-reviewers", "4", "update", `[` + condition + `]`, `{"requiredApprovals":2}`, "PUT", conditionPath + "/condition/4", `{"sourceMatcher":{"id":"any","type":{"id":"ANY_REF"}},"targetMatcher":{"id":"refs/heads/main","type":{"id":"BRANCH"}},"reviewers":[{"id":123}],"requiredApprovals":2,"reviewerGroups":[{"id":12}]}`},
		{"create condition", "default-reviewers", "", "create", "", `{"sourceMatcher":{"id":"any","type":{"id":"ANY_REF"}},"targetMatcher":{"id":"any","type":{"id":"ANY_REF"}},"reviewers":[{"id":123}],"requiredApprovals":1}`, "POST", conditionPath + "/condition", `{"sourceMatcher":{"id":"any","type":{"id":"ANY_REF"}},"targetMatcher":{"id":"any","type":{"id":"ANY_REF"}},"reviewers":[{"id":123}],"requiredApprovals":1}`},
		{"delete condition", "default-reviewers", "4", "delete", `[` + condition + `]`, "", "DELETE", conditionPath + "/condition/4", ""},
		{"restore auto decline inheritance", "auto-decline", "", "delete", "", "", "DELETE", "/bitbucket/rest/api/1.0/projects/PRJ/repos/repo/settings/auto-decline", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writes := 0
			svc := settingsService(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("X-Atlassian-Token") != "no-check" {
					t.Error("missing authentication/XSRF headers")
				}
				if r.Method == "GET" {
					expected := tt.path
					if tt.resource == "default-reviewers" {
						expected = conditionPath + "/conditions"
					}
					if r.URL.Path != expected {
						t.Errorf("GET path %s, want %s", r.URL.Path, expected)
					}
					fmt.Fprint(w, tt.current)
					return
				}
				writes++
				if r.Method != tt.method || r.URL.Path != tt.path {
					t.Errorf("write %s %s; want %s %s", r.Method, r.URL.Path, tt.method, tt.path)
				}
				if tt.expected != "" {
					body, _ := io.ReadAll(r.Body)
					assertSettingsJSON(t, body, tt.expected)
				}
				w.WriteHeader(http.StatusNoContent)
			})
			if err := api.ChangeRepoSetting(svc, "PRJ", "repo", tt.resource, tt.id, tt.action, json.RawMessage(tt.patch)); err != nil {
				t.Fatal(err)
			}
			if writes != 1 {
				t.Errorf("writes = %d, want 1", writes)
			}
		})
	}
}

func TestReadReviewerConditionsPaginatesAndProtectsInheritedConditions(t *testing.T) {
	writes := 0
	svc := settingsService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes++
		}
		if r.URL.Query().Get("start") == "0" {
			fmt.Fprint(w, `{"values":[{"id":1,"scope":{"type":"PROJECT"}}],"isLastPage":false,"nextPageStart":17}`)
		} else if r.URL.Query().Get("start") == "17" {
			fmt.Fprint(w, `{"values":[{"id":2,"scope":{"type":"REPOSITORY"}}],"isLastPage":true}`)
		} else {
			t.Errorf("unexpected pagination %s", r.URL.RawQuery)
		}
	})
	raw, err := api.ReadRepoSetting(svc, "PRJ", "repo", "default-reviewers", "")
	if err != nil {
		t.Fatal(err)
	}
	assertSettingsJSON(t, raw, `[{"id":1,"scope":{"type":"PROJECT"}},{"id":2,"scope":{"type":"REPOSITORY"}}]`)
	for _, action := range []string{"update", "delete"} {
		err := api.ChangeRepoSetting(svc, "PRJ", "repo", "default-reviewers", "1", action, json.RawMessage(`{"requiredApprovals":2}`))
		if err == nil || !strings.Contains(err.Error(), "inherited") {
			t.Fatalf("want inherited condition error, got %v", err)
		}
	}
	if writes != 0 {
		t.Fatal("wrote inherited condition")
	}
}

func TestRepoSettingErrorsNeverFallBackOrRetry(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 409, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			writes := 0
			svc := settingsService(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					fmt.Fprint(w, `{"requiredAllTasksComplete":false}`)
					return
				}
				writes++
				w.WriteHeader(status)
				fmt.Fprint(w, `{"errors":[{"message":"server explanation"}]}`)
			})
			err := api.ChangeRepoSetting(svc, "PRJ", "repo", "pull-requests", "", "update", json.RawMessage(`{"requiredAllTasksComplete":true}`))
			if err == nil || !strings.Contains(err.Error(), "server explanation") || writes != 1 {
				t.Fatalf("writes=%d err=%v", writes, err)
			}
		})
	}
}

func TestRepoSettingInvalidRequestsDoNotWrite(t *testing.T) {
	writes := 0
	svc := settingsService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes++
		}
		fmt.Fprint(w, `{}`)
	})
	for _, tt := range []struct{ resource, id, action, patch string }{
		{"invented", "", "update", `{}`},
		{"pull-requests", "", "delete", ``},
		{"pull-requests", "", "create", `{}`},
		{"pull-requests", "", "update", `null`},
		{"pull-requests", "", "update", `[]`},
		{"pull-requests", "4", "update", `{}`},
		{"default-reviewers", "../4", "delete", ``},
		{"default-reviewers", "", "create", `{}`},
		{"pull-requests", "", "update", fmt.Sprintf(`{"requiredApprovers":1,"%s":{"count":2}}`, api.RequiredApproversSetting)},
	} {
		if err := api.ChangeRepoSetting(svc, "PRJ", "repo", tt.resource, tt.id, tt.action, json.RawMessage(tt.patch)); err == nil {
			t.Errorf("expected error for %+v", tt)
		}
	}
	cloud, err := api.NewService("cloud.invalid", config.Host{Type: "cloud", APIBase: "https://cloud.invalid/2.0"})
	if err != nil {
		t.Fatal(err)
	}
	if api.CanEditRepoSettings(cloud) {
		t.Fatal("cloud claims editing support")
	}
	if err := api.ChangeRepoSetting(cloud, "w", "r", "pull-requests", "", "update", json.RawMessage(`{}`)); err == nil {
		t.Fatal("cloud should reject without making a request")
	}
	if writes != 0 {
		t.Fatalf("invalid requests wrote %d times", writes)
	}
}

func TestRepoSettingsPanelsDoNotGuessUnsupportedEndpointsOrMergeDefaults(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	svc := settingsService(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"errors":[{"message":"not permitted"}]}`)
	})
	settings, err := svc.RepoSettings("PRJ", "repo")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Reviewer groups", "Description template", "Audit log", "Code Insights", "HTTP access tokens"} {
		if !strings.Contains(settings.Panels[name].Hint, "Unsupported") {
			t.Errorf("%s not marked unsupported: %+v", name, settings.Panels[name])
		}
	}
	if settings.Panels["Merge strategies"].Error == "" || len(settings.Panels["Merge strategies"].Items) != 0 {
		t.Fatal("merge strategy failure must not be replaced by guessed defaults")
	}
	for _, path := range paths {
		for _, bad := range []string{"access-tokens", "/rest/ssh/latest/projects/", "/settings/hooks/plugin:hook/settings", "reviewer-groups", "description-template", "/audit/", "/pull-requests/git", "/pull-requests/auto-decline", "required-builds"} {
			if strings.Contains(path, bad) {
				t.Errorf("requested unsupported endpoint: %s", path)
			}
		}
	}
}
