//go:build unix

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hugs7/bitbucket-cli/internal/config"
)

type overrideFixture struct {
	t                  *testing.T
	svc                *serverService
	dir                string
	settings           map[string]any
	conditions         []map[string]any
	hooks              []map[string]any
	hookSettings       map[string]any
	inheritedHooks     map[string]map[string]any
	inheritanceDeletes int
	mergeFailure       bool
	disableFailure     bool
	restoreFailure     bool
	restoreIgnored     bool
	permissionDenied   bool
	bypass             bool
	mergeCalls         int
	settingsWrites     int
	dependentScans     int
	journalChecks      int
}

func newOverrideFixture(t *testing.T) *overrideFixture {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir) // os.UserConfigDir uses HOME rather than XDG on macOS.
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	f := &overrideFixture{t: t, dir: configDir, bypass: true}
	f.settings = map[string]any{
		"requiredApprovers":  float64(2),
		overrideApproversKey: map[string]any{"enable": true, "count": float64(2)},
		"unrelated":          "preserve me",
	}
	f.conditions = []map[string]any{{
		"id": float64(7), "scope": map[string]any{"type": "REPOSITORY", "resourceId": float64(1)},
		"sourceRefMatcher": map[string]any{"id": "any", "type": map[string]any{"id": "ANY_REF"}},
		"targetRefMatcher": map[string]any{"id": "refs/heads/main", "type": map[string]any{"id": "BRANCH"}},
		"reviewers":        []any{map[string]any{"id": float64(10), "name": "reviewer"}}, "requiredApprovals": float64(1),
	}}
	f.hooks = []map[string]any{
		{"details": map[string]any{"key": "com.atlassian.bitbucket.server.bitbucket-bundled-hooks:requiredApproversMergeHook", "type": "PRE_PULL_REQUEST_MERGE"}, "enabled": true, "configured": true, "scope": map[string]any{"type": "REPOSITORY", "resourceId": float64(1)}},
		{"details": map[string]any{"key": "example:check", "type": "PRE_PULL_REQUEST_MERGE"}, "enabled": true, "configured": true, "scope": map[string]any{"type": "REPOSITORY", "resourceId": float64(1)}},
		{"details": map[string]any{"key": "example:push", "type": "PRE_RECEIVE"}, "enabled": true, "configured": true, "scope": map[string]any{"type": "PROJECT", "resourceId": float64(9)}},
	}
	f.hookSettings = map[string]any{"threshold": float64(2), "retain": "original"}
	server := httptest.NewServer(http.HandlerFunc(f.serveHTTP))
	t.Cleanup(server.Close)
	f.svc = &serverService{client: New("test", config.Host{Type: "server", APIBase: server.URL + "/rest/api/latest"})}
	return f
}

func (f *overrideFixture) inheritHook(index int) {
	if f.inheritedHooks == nil {
		f.inheritedHooks = make(map[string]map[string]any)
	}
	hook := f.hooks[index]
	hook["scope"] = map[string]any{"type": "PROJECT", "resourceId": float64(9)}
	key := hook["details"].(map[string]any)["key"].(string)
	f.inheritedHooks[key] = overrideCopy(hook)
}

func (f *overrideFixture) assertJournal() {
	f.t.Helper()
	files, err := filepath.Glob(filepath.Join(f.dir, "bb", "merge-recovery", "*.json"))
	if err != nil || len(files) != 1 {
		f.t.Errorf("remote write without one recovery journal: %v %v", files, err)
		return
	}
	info, err := os.Stat(files[0])
	if err != nil || info.Mode().Perm() != 0600 {
		f.t.Errorf("journal is not private: %v %v", info, err)
	}
	j, err := readOverrideJournal(files[0])
	if err != nil || j.Settings["requiredApprovers"] != float64(2) || len(j.Hooks) != 2 || len(j.Conditions) != 1 {
		f.t.Errorf("journal missing original snapshots: %#v %v", j, err)
		return
	}
	for key, original := range f.inheritedHooks {
		found := false
		for _, hook := range j.Hooks {
			if hook.Key == key {
				found = reflect.DeepEqual(hook.State, original)
			}
		}
		if !found {
			f.t.Errorf("journal lost original inherited scope/state for %s", key)
		}
	}
	f.journalChecks++
}

func (f *overrideFixture) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	path := r.URL.Path
	var response any = map[string]any{}
	if r.Method != http.MethodGet && f.bypass {
		f.assertJournal()
		if !strings.Contains(path, "/projects/PROJ/repos/repo/") {
			f.t.Errorf("write outside repository scope: %s %s", r.Method, path)
			http.Error(w, "project writes forbidden", http.StatusForbidden)
			return
		}
	}
	var body map[string]any
	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		if r.Header.Get("Content-Type") != "application/json" {
			f.t.Errorf("missing JSON Content-Type: %s %s", r.Method, path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.t.Errorf("invalid JSON body: %v", err)
		}
	}
	switch {
	case strings.HasSuffix(path, "/permissions/users"):
		if !f.bypass {
			f.t.Error("default merge inspected admin settings")
		}
		if f.permissionDenied {
			http.Error(w, "not admin", http.StatusForbidden)
			return
		}
	case strings.HasSuffix(path, "/pull-requests/1/merge"):
		f.mergeCalls++
		if f.bypass && (f.settings["requiredApprovers"] != float64(0) || f.conditions[0]["requiredApprovals"] != float64(0) || f.hooks[1]["enabled"] != false) {
			f.t.Error("merge occurred before disabling and verifying settings")
		}
		if body["strategyId"] != "squash" {
			f.t.Errorf("strategy not preserved: %v", body)
		}
		if _, exists := body["close_source_branch"]; exists {
			f.t.Error("merge must not request source deletion")
		}
		if f.mergeFailure {
			http.Error(w, "merge veto", http.StatusConflict)
			return
		}
	case strings.HasSuffix(path, "/pull-requests/1"):
		response = map[string]any{"id": 1, "version": 3, "state": "OPEN", "fromRef": map[string]any{"id": "refs/heads/feature", "displayId": "feature"}, "toRef": map[string]any{"id": "refs/heads/main", "displayId": "main"}}
	case strings.HasSuffix(path, "/settings/pull-requests"):
		if r.Method == http.MethodPost {
			f.settingsWrites++
			if len(body) != 1 {
				f.t.Errorf("must use only scalar requiredApprovers, not plugin count: %v", body)
			}
			n := body["requiredApprovers"].(float64)
			f.settings["requiredApprovers"] = n
			// The scalar endpoint creates a local hook override, including
			// when restoring an originally project-inherited approver check.
			f.hooks[0]["scope"] = map[string]any{"type": "REPOSITORY", "resourceId": float64(1)}
			f.hooks[0]["enabled"] = n > 0
			plugin := f.settings[overrideApproversKey].(map[string]any)
			plugin["enable"] = n > 0
			if n > 0 {
				plugin["count"] = n
			} else {
				plugin["count"] = float64(9) // Westpac may retain/report a different disabled count.
			}
		}
		response = f.settings
	case strings.HasSuffix(path, "/settings/hooks"):
		response = map[string]any{"values": f.hooks, "isLastPage": true}
	case strings.Contains(path, "/settings/hooks/"):
		key := strings.Split(strings.Split(path, "/settings/hooks/")[1], "/")[0]
		var hook map[string]any
		for _, h := range f.hooks {
			if h["details"].(map[string]any)["key"] == key {
				hook = h
			}
		}
		if hook == nil || key == "example:push" {
			f.t.Errorf("unrelated/unknown hook touched: %s", path)
			http.NotFound(w, r)
			return
		}
		response = hook
		if strings.HasSuffix(path, "/settings") {
			response = f.hookSettings
		}
		if r.Method == http.MethodDelete && strings.HasSuffix(path, "/"+key) {
			f.settingsWrites++
			f.inheritanceDeletes++
			original := f.inheritedHooks[key]
			if original == nil {
				f.t.Errorf("removed a repository-local hook configuration: %s", key)
				http.Error(w, "not inherited", http.StatusBadRequest)
				return
			}
			if f.restoreFailure && key == "example:check" {
				http.Error(w, "inheritance restore unavailable", http.StatusServiceUnavailable)
				return
			}
			if f.restoreIgnored && key == "example:check" {
				hook["enabled"] = true // Enabled is insufficient: scope must also be restored.
			} else {
				for k, value := range original {
					hook[k] = value
				}
			}
		}
		if strings.HasSuffix(path, "/enabled") {
			f.settingsWrites++
			if r.Method == http.MethodDelete {
				hook["enabled"] = false
				hook["scope"] = map[string]any{"type": "REPOSITORY", "resourceId": float64(1)}
				if f.disableFailure && key == "example:check" {
					http.Error(w, "disable applied but response failed", http.StatusInternalServerError)
					return
				}
			} else if r.Method == http.MethodPut {
				if f.inheritedHooks[key] != nil {
					f.t.Errorf("restored inherited hook by enabling a local override: %s", key)
				}
				if !reflect.DeepEqual(body, f.hookSettings) {
					f.t.Errorf("hook configuration lost: %v", body)
				}
				if f.restoreFailure && key == "example:check" {
					http.Error(w, "restore unavailable", http.StatusServiceUnavailable)
					return
				}
				if !f.restoreIgnored || key != "example:check" {
					hook["enabled"] = true
				}
			}
		}
	case strings.HasSuffix(path, "/conditions"):
		response = f.conditions // Verified Westpac shape is a bare array.
	case strings.HasSuffix(path, "/condition/7"):
		f.settingsWrites++
		if !reflect.DeepEqual(body["reviewers"], f.conditions[0]["reviewers"]) || !reflect.DeepEqual(body["sourceMatcher"], f.conditions[0]["sourceRefMatcher"]) || !reflect.DeepEqual(body["targetMatcher"], f.conditions[0]["targetRefMatcher"]) {
			f.t.Errorf("reviewers or matchers lost: %v", body)
		}
		f.conditions[0]["requiredApprovals"] = body["requiredApprovals"]
		response = f.conditions[0]
	case strings.HasSuffix(path, "/pull-requests"):
		f.dependentScans++
		if f.hooks[1]["enabled"] != true || f.settings["requiredApprovers"] != float64(2) || f.conditions[0]["requiredApprovals"] != float64(1) {
			f.t.Error("dependent scan occurred before settings restoration")
		}
		for _, hook := range f.hooks {
			key := hook["details"].(map[string]any)["key"].(string)
			if original := f.inheritedHooks[key]; original != nil && !reflect.DeepEqual(hook, original) {
				f.t.Error("dependent scan occurred before hook inheritance restoration")
			}
		}
		response = map[string]any{"values": []any{}, "isLastPage": true}
	default:
		f.t.Errorf("unexpected request (including branch deletion): %s %s", r.Method, path)
		http.NotFound(w, r)
		return
	}
	_ = json.NewEncoder(w).Encode(response)
}

func TestMergeOverrideRestoration(t *testing.T) {
	for _, scenario := range []struct {
		name                                                         string
		mergeFailure, disableFailure, restoreFailure, restoreIgnored bool
		inherited, inheritedApprovers                                bool
		wantError                                                    string
		wantMerges, wantScans                                        int
	}{
		{name: "success", wantMerges: 1, wantScans: 1},
		{name: "merge failure", mergeFailure: true, wantError: "outcome uncertain", wantMerges: 1},
		{name: "partial disable applied then failed", disableFailure: true, wantError: "merge not attempted"},
		{name: "restore failure", restoreFailure: true, wantError: "PR merged; settings restoration failed", wantMerges: 1},
		{name: "restore silently ignored", restoreIgnored: true, wantError: "state verification failed", wantMerges: 1},
		{name: "inherited success", inherited: true, wantMerges: 1, wantScans: 1},
		{name: "inherited approvers success", inherited: true, inheritedApprovers: true, wantMerges: 1, wantScans: 1},
		{name: "inherited merge failure", inherited: true, inheritedApprovers: true, mergeFailure: true, wantError: "outcome uncertain", wantMerges: 1},
		{name: "inherited partial disable failure", inherited: true, inheritedApprovers: true, disableFailure: true, wantError: "merge not attempted"},
		{name: "inherited restore failure", inherited: true, inheritedApprovers: true, restoreFailure: true, wantError: "project inheritance", wantMerges: 1},
		{name: "inherited restore enabled but wrong scope", inherited: true, restoreIgnored: true, wantError: "state verification failed", wantMerges: 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f := newOverrideFixture(t)
			if scenario.inherited {
				f.inheritHook(1)
			}
			if scenario.inheritedApprovers {
				f.inheritHook(0)
			}
			f.mergeFailure, f.disableFailure, f.restoreFailure, f.restoreIgnored = scenario.mergeFailure, scenario.disableFailure, scenario.restoreFailure, scenario.restoreIgnored
			_, err := MergePullRequest(f.svc, "PROJ", "repo", 1, MergeOptions{BypassChecks: true, RetargetDependents: true, StrategyID: "squash"})
			if scenario.wantError == "" && err != nil || scenario.wantError != "" && (err == nil || !strings.Contains(err.Error(), scenario.wantError)) {
				t.Fatalf("error = %v, want %q", err, scenario.wantError)
			}
			if f.mergeCalls != scenario.wantMerges || f.dependentScans != scenario.wantScans {
				t.Fatalf("merge/scan calls = %d/%d, want %d/%d", f.mergeCalls, f.dependentScans, scenario.wantMerges, scenario.wantScans)
			}
			if f.journalChecks == 0 || f.settings["requiredApprovers"] != float64(2) || f.conditions[0]["requiredApprovals"] != float64(1) {
				t.Fatal("cleanup did not restore independent settings")
			}
			if f.inheritanceDeletes != len(f.inheritedHooks) {
				t.Fatalf("inheritance reset attempts = %d, want %d", f.inheritanceDeletes, len(f.inheritedHooks))
			}
			files, _ := filepath.Glob(filepath.Join(f.dir, "bb", "merge-recovery", "*.json"))
			if scenario.restoreFailure || scenario.restoreIgnored {
				if len(files) != 1 || !strings.Contains(err.Error(), files[0]) {
					t.Fatalf("missing recoverable journal/error: %v %v", files, err)
				}
				command := "bb pr recover-merge-checks -R test/PROJ/repo"
				if !strings.Contains(err.Error(), command) || strings.Contains(err.Error(), "RecoverMergeChecks") {
					t.Fatalf("restoration error lacks concrete recovery command: %v", err)
				}
				before := f.settingsWrites
				if _, err := MergePullRequest(f.svc, "PROJ", "repo", 1, MergeOptions{BypassChecks: true}); err == nil || !strings.Contains(err.Error(), command) || f.settingsWrites != before {
					t.Fatalf("unresolved journal did not block another bypass with recovery command: %v", err)
				}
				f.restoreFailure, f.restoreIgnored = false, false
				if err := f.svc.RecoverMergeChecks("PROJ", "repo"); err != nil {
					t.Fatal(err)
				}
				if f.hooks[1]["enabled"] != true || f.mergeCalls != scenario.wantMerges {
					t.Fatal("recovery failed or attempted another merge")
				}
				files, _ = filepath.Glob(filepath.Join(f.dir, "bb", "merge-recovery", "*.json"))
			}
			if len(files) != 0 {
				t.Fatalf("verified restoration left journal: %v", files)
			}
			for _, hook := range f.hooks {
				key := hook["details"].(map[string]any)["key"].(string)
				if original := f.inheritedHooks[key]; original != nil && !reflect.DeepEqual(hook, original) {
					t.Fatalf("inherited hook %s did not match its complete original snapshot", key)
				}
			}
		})
	}
}

func TestMergeOverrideDefaultDoesNotBypass(t *testing.T) {
	f := newOverrideFixture(t)
	f.bypass = false
	if _, err := MergePullRequest(f.svc, "PROJ", "repo", 1, MergeOptions{StrategyID: "squash"}); err != nil {
		t.Fatal(err)
	}
	if f.settingsWrites != 0 || f.mergeCalls != 1 {
		t.Fatalf("default merge performed settings changes: writes=%d merges=%d", f.settingsWrites, f.mergeCalls)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "bb", "merge-recovery")); !os.IsNotExist(err) {
		t.Fatal("default merge created recovery state")
	}
}

func TestMergeOverrideRefusesUnsafeConfiguration(t *testing.T) {
	for _, scenario := range []string{"not admin", "global hook", "inherited reviewers", "unknown matcher", "missing plugin", "unknown approval hook", "unknown hook scope", "missing project scope ID"} {
		t.Run(scenario, func(t *testing.T) {
			f := newOverrideFixture(t)
			switch scenario {
			case "not admin":
				f.permissionDenied = true
			case "global hook":
				f.hooks[1]["scope"].(map[string]any)["type"] = "GLOBAL"
			case "inherited reviewers":
				f.conditions[0]["scope"].(map[string]any)["type"] = "PROJECT"
			case "unknown matcher":
				f.conditions[0]["targetRefMatcher"].(map[string]any)["type"] = map[string]any{"id": "MODEL_BRANCH"}
			case "missing plugin":
				delete(f.settings, overrideApproversKey)
			case "unknown approval hook":
				f.hooks = f.hooks[1:]
			case "unknown hook scope":
				delete(f.hooks[1], "scope")
			case "missing project scope ID":
				f.hooks[1]["scope"] = map[string]any{"type": "PROJECT"}
			}
			_, err := MergePullRequest(f.svc, "PROJ", "repo", 1, MergeOptions{BypassChecks: true})
			if err == nil || f.settingsWrites != 0 || f.mergeCalls != 0 {
				t.Fatalf("unsafe configuration accepted: %v writes=%d merges=%d", err, f.settingsWrites, f.mergeCalls)
			}
		})
	}
}

func TestMergeOverrideLockAndInterruptedRecovery(t *testing.T) {
	f := newOverrideFixture(t)
	f.inheritHook(0)
	f.inheritHook(1)
	path, lock, err := f.svc.lockMergeOverride("PROJ", "repo")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RecoverMergeChecks("PROJ", "repo"); err == nil || !strings.Contains(err.Error(), "lock") {
		t.Fatalf("concurrent recovery not rejected: %v", err)
	}
	j, err := f.svc.snapshotMergeOverride("PROJ", "repo", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeOverrideJournal(path, j); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.disableMergeOverride(j); err != nil {
		t.Fatal(err)
	}
	lock.Close() // Simulates process exit without its cleanup defer.
	if err := f.svc.RecoverMergeChecks("PROJ", "repo"); err != nil {
		t.Fatal(err)
	}
	if f.mergeCalls != 0 || f.hooks[1]["enabled"] != true || f.settings["requiredApprovers"] != float64(2) {
		t.Fatal("interrupted recovery did not restore without merging")
	}
	if f.inheritanceDeletes != 2 || overrideScope(f.hooks[0]) != "PROJECT" || overrideScope(f.hooks[1]) != "PROJECT" {
		t.Fatal("interrupted recovery did not restore project inheritance")
	}
}

func TestMergeOverrideUnsupportedBackend(t *testing.T) {
	svc := &mergeServiceStub{}
	_, err := MergePullRequest(svc, "PROJ", "repo", 1, MergeOptions{BypassChecks: true})
	if err == nil || !strings.Contains(err.Error(), "unsupported") || svc.merged {
		t.Fatalf("unsupported capability result: %v merged=%t", err, svc.merged)
	}
}

func TestMergeOverrideCollectionPagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("start") == "0" {
			fmt.Fprint(w, `{"values":[{"id":1}],"isLastPage":false,"nextPageStart":17}`)
		} else if r.URL.Query().Get("start") == "17" {
			fmt.Fprint(w, `{"values":[{"id":2}],"isLastPage":true}`)
		} else {
			t.Errorf("incorrect pagination: %s", r.URL)
		}
	}))
	defer server.Close()
	svc := &serverService{client: New("test", config.Host{APIBase: server.URL})}
	rows, err := svc.overrideCollection(server.URL + "/collection")
	if err != nil || len(rows) != 2 {
		t.Fatalf("paginated collection = %v, %v", rows, err)
	}
}
