package api_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hugs7/bitbucket-cli/internal/api"
)

func TestAdminSettingsDocumentedWrites(t *testing.T) {
	const base = "/bitbucket/rest/api/1.0/projects/PRJ/repos/repo"
	const keys = "/bitbucket/rest/keys/1.0/projects/PRJ/repos/repo/ssh"
	const restrictions = "/bitbucket/rest/branch-permissions/2.0/projects/PRJ/repos/repo/restrictions"
	for _, tt := range []struct {
		name, resource, action, id, patch, current, method, path, query, body string
	}{
		{"metadata preserves writable fields without moving repo", "details", "update", "", `{"description":"new","public":false}`, `{"id":99,"name":"Repo","description":"old","forkable":true,"public":true,"project":{"key":"PRJ"},"links":{}}`, "PUT", base, "", `{"name":"Repo","description":"new","forkable":true,"public":false}`},
		{"default branch", "default-branch", "update", "", `{"id":"refs/heads/release/next"}`, "", "PUT", base + "/default-branch", "", `{"id":"refs/heads/release/next"}`},
		{"grant user", "permissions-users", "create", "a+b&c", `{"permission":"REPO_WRITE"}`, "", "PUT", base + "/permissions/users", "name=a%2Bb%26c&permission=REPO_WRITE", ""},
		{"change group", "permissions-groups", "update", "Dev Team", `{"permission":"REPO_ADMIN"}`, "", "PUT", base + "/permissions/groups", "name=Dev+Team&permission=REPO_ADMIN", ""},
		{"revoke user", "permissions-users", "delete", "alice", "", "", "DELETE", base + "/permissions/users", "name=alice", ""},
		{"revoke group", "permissions-groups", "delete", "Dev Team", "", "", "DELETE", base + "/permissions/groups", "name=Dev+Team", ""},
		{"create restriction", "branch-restrictions", "create", "", `{"type":"no-deletes","matcher":{"id":"refs/heads/main","type":{"id":"BRANCH"}},"users":["alice"],"groups":["devs"],"accessKeys":[7]}`, "", "POST", restrictions, "", `{"type":"no-deletes","matcher":{"id":"refs/heads/main","type":{"id":"BRANCH"}},"users":["alice"],"groups":["devs"],"accessKeys":[7]}`},
		{"delete restriction", "branch-restrictions", "delete", "8", "", "", "DELETE", restrictions + "/8", "", ""},
		{"register public key", "access-keys", "create", "", `{"key":{"text":"ssh-ed25519 AAAA example","label":"CI"},"permission":"REPO_READ"}`, "", "POST", keys, "", `{"key":{"text":"ssh-ed25519 AAAA example","label":"CI"},"permission":"REPO_READ"}`},
		{"change key permission does not replace key", "access-keys", "update", "7", `{"permission":"REPO_WRITE"}`, "", "PUT", keys + "/7/permission/REPO_WRITE", "", ""},
		{"revoke repository key only", "access-keys", "delete", "7", "", "", "DELETE", keys + "/7", "", ""},
		{"enable hook without resetting config", "hooks", "enable", "plugin:hook", "", "", "PUT", base + "/settings/hooks/plugin:hook/enabled", "", ""},
		{"disable hook", "hooks", "disable", "plugin:hook", "", "", "DELETE", base + "/settings/hooks/plugin:hook/enabled", "", ""},
		{"hook config preserves unedited fields", "hook-settings", "update", "plugin:hook", `{"rules":{"count":3}}`, `{"rules":{"count":1,"future":true},"credential":"existing-secret","pluginVersion":2}`, "PUT", base + "/settings/hooks/plugin:hook/settings", "", `{"rules":{"count":3,"future":true},"credential":"existing-secret","pluginVersion":2}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			writes := 0
			svc := settingsService(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					if tt.current == "" || r.URL.Path != tt.path {
						t.Errorf("unexpected GET %s", r.URL.Path)
					}
					fmt.Fprint(w, tt.current)
					return
				}
				writes++
				if r.Method != tt.method || r.URL.Path != tt.path || r.URL.RawQuery != tt.query {
					t.Errorf("got %s %s; want %s %s?%s", r.Method, r.URL, tt.method, tt.path, tt.query)
				}
				body, _ := io.ReadAll(r.Body)
				if tt.body != "" {
					assertSettingsJSON(t, body, tt.body)
				} else if len(body) != 0 {
					t.Errorf("expected no body, got %s", body)
				}
				w.WriteHeader(http.StatusNoContent)
			})
			if err := api.ChangeRepoSetting(svc, "PRJ", "repo", tt.resource, tt.id, tt.action, json.RawMessage(tt.patch)); err != nil {
				t.Fatal(err)
			}
			if writes != 1 {
				t.Errorf("writes=%d", writes)
			}
		})
	}
}

func TestAdminSettingsRejectUnsupportedOrUnsafeWrites(t *testing.T) {
	calls := 0
	svc := settingsService(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `{}`)
	})
	for _, tt := range []struct{ resource, action, id, body string }{
		{"details", "delete", "", ""},
		{"details", "update", "", `{"project":{"key":"OTHER"}}`},
		{"details", "update", "", `{"slug":"other"}`},
		{"default-branch", "update", "", `{"id":"main"}`},
		{"permissions-users", "update", "", `{"permission":"REPO_READ"}`},
		{"permissions-users", "update", "alice", `{"permission":"SYS_ADMIN"}`},
		{"branch-restrictions", "update", "7", `{}`},
		{"branch-restrictions", "delete", "../7", ""},
		{"access-keys", "create", "", `{"key":{"text":"-----BEGIN OPENSSH PRIVATE KEY-----"},"permission":"REPO_READ"}`},
		{"access-keys", "create", "", `{"key":{"text":"ssh-rsa public"},"permission":"REPO_ADMIN"}`},
		{"access-keys", "update", "7", `{"key":{"label":"new"},"permission":"REPO_READ"}`},
		{"hooks", "enable", "../x", ""},
		{"hook-settings", "update", "", `{}`},
		{"access-tokens", "create", "", `{}`},
	} {
		if err := api.ChangeRepoSetting(svc, "PRJ", "repo", tt.resource, tt.id, tt.action, json.RawMessage(tt.body)); err == nil {
			t.Errorf("expected rejection for %+v", tt)
		}
	}
	if calls != 0 {
		t.Errorf("unsafe requests made %d HTTP calls", calls)
	}
}

func TestAdminSettingsReadCollectionsAndDetails(t *testing.T) {
	for _, resource := range []string{"permissions-users", "permissions-groups", "branch-restrictions", "access-keys", "hooks"} {
		t.Run(resource, func(t *testing.T) {
			calls := 0
			svc := settingsService(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || strings.Contains(r.URL.Path, "access-tokens") || strings.HasSuffix(r.URL.Path, "/settings") {
					t.Errorf("unsafe read %s %s", r.Method, r.URL)
				}
				if r.URL.Query().Get("start") == "0" {
					fmt.Fprint(w, `{"values":[{"id":1}],"isLastPage":false,"nextPageStart":7}`)
				} else if r.URL.Query().Get("start") == "7" {
					fmt.Fprint(w, `{"values":[{"id":2}],"isLastPage":true}`)
				} else {
					t.Errorf("unexpected page %s", r.URL)
				}
			})
			raw, err := api.ReadRepoSetting(svc, "PRJ", "repo", resource, "")
			if err != nil {
				t.Fatal(err)
			}
			assertSettingsJSON(t, raw, `[{"id":1},{"id":2}]`)
			if calls != 2 {
				t.Fatalf("calls=%d", calls)
			}
		})
	}
}

func TestAdminSettingsReadFailureBlocksPreservingUpdates(t *testing.T) {
	for _, resource := range []string{"details", "hook-settings"} {
		t.Run(resource, func(t *testing.T) {
			writes := 0
			svc := settingsService(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					writes++
				}
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, `{"errors":[{"message":"cannot read existing settings"}]}`)
			})
			id, patch := "", `{"description":"new"}`
			if resource == "hook-settings" {
				id, patch = "plugin:hook", `{"x":true}`
			}
			err := api.ChangeRepoSetting(svc, "PRJ", "repo", resource, id, "update", json.RawMessage(patch))
			if err == nil || !strings.Contains(err.Error(), "cannot read existing settings") || writes != 0 {
				t.Fatalf("err=%v writes=%d", err, writes)
			}
		})
	}
}

func TestSettingsOverviewDoesNotFetchTokensOrHookConfiguration(t *testing.T) {
	svc := settingsService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || strings.Contains(r.URL.Path, "token") || strings.HasSuffix(r.URL.Path, "/settings") {
			t.Errorf("unsafe overview request: %s %s", r.Method, r.URL)
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/settings/pull-requests"):
			fmt.Fprint(w, westpacApprovals)
		case strings.HasSuffix(r.URL.Path, "/repos/repo"):
			fmt.Fprint(w, `{"id":1,"slug":"repo","name":"Repo","project":{"key":"PRJ"}}`)
		case strings.HasSuffix(r.URL.Path, "/default-branch"):
			fmt.Fprint(w, `{"id":"refs/heads/main"}`)
		case strings.HasSuffix(r.URL.Path, "/auto-decline"):
			fmt.Fprint(w, `{"enabled":false,"inactivityWeeks":4}`)
		default:
			fmt.Fprint(w, `{"values":[],"isLastPage":true}`)
		}
	})
	settings, err := svc.RepoSettings("PRJ", "repo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(settings.Panels["HTTP access tokens"].Hint, "Unsupported for safety") {
		t.Fatal("token capability must be explicitly unsupported")
	}
	if !strings.Contains(settings.Panels["Merge checks"].Hint, "Current minimum approving reviewers: 0") {
		t.Fatal("overview must prioritize requiredApprovers:0 over disabled plugin count:1")
	}
}
