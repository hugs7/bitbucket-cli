package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hugs7/bitbucket-cli/internal/api"
	"github.com/hugs7/bitbucket-cli/internal/config"
)

func TestRepoSettingsCLIConfirmationAndMinimumApprovals(t *testing.T) {
	for _, tt := range []struct {
		name, input string
		yes         bool
		writes      int
		minimum     int
	}{
		{"cancel", "no\n", false, 0, 2},
		{"empty input is not confirmation", "", false, 0, 2},
		{"explicit confirmation", "yes\n", false, 1, 2},
		{"flag confirmation", "", true, 1, 2},
		{"Westpac disable sends scalar zero only", "", true, 1, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			writes := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/rest/api/1.0/projects/PRJ/repos/repo/settings/pull-requests" {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				if r.Method == "GET" {
					fmt.Fprintf(w, `{"requiredApprovers":0,"%s":{"enable":false,"count":1},"mergeConfig":{"type":"PROJECT"}}`, api.WestpacRequiredApproversSetting)
					return
				}
				writes++
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if r.Method != "POST" || len(body) != 1 || string(body["requiredApprovers"]) != fmt.Sprint(tt.minimum) {
					t.Errorf("unexpected write %s %+v", r.Method, body)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			configureSettingsTestHost(t, server.URL)
			c := newRepoSettingsCmd()
			args := []string{"update", "pull-requests", "-R", "settings.test/PRJ/repo", "--minimum-approvals", fmt.Sprint(tt.minimum)}
			if tt.yes {
				args = append(args, "--yes")
			}
			c.SetArgs(args)
			c.SetIn(strings.NewReader(tt.input))
			var out bytes.Buffer
			c.SetOut(&out)
			c.SetErr(&out)
			err := c.Execute()
			if tt.writes == 0 && (err == nil || !strings.Contains(err.Error(), "cancelled")) {
				t.Errorf("expected cancellation, got %v", err)
			}
			if tt.writes == 1 && err != nil {
				t.Fatal(err)
			}
			if writes != tt.writes {
				t.Errorf("writes = %d, want %d", writes, tt.writes)
			}
		})
	}
}

func configureSettingsTestHost(t *testing.T, base string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	data := fmt.Sprintf("hosts:\n  settings.test:\n    type: server\n    api_base: %s/rest/api/1.0\n", base)
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if err := config.Load(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.Load(filepath.Join(dir, "empty.yml")) })
}

func TestRepoSettingsCLIGetAndJSONUpdate(t *testing.T) {
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			fmt.Fprint(w, `{"enabled":true,"inactivityWeeks":4}`)
			return
		}
		writes++
		if r.Method != "PUT" || !strings.HasSuffix(r.URL.Path, "/settings/auto-decline") {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"errors":[{"message":"admin permission required"}]}`)
	}))
	defer server.Close()
	configureSettingsTestHost(t, server.URL)
	c := newRepoSettingsCmd()
	c.SetArgs([]string{"get", "auto-decline", "-R", "settings.test/PRJ/repo"})
	var out bytes.Buffer
	c.SetOut(&out)
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out.Bytes()) || !strings.Contains(out.String(), `"inactivityWeeks": 4`) || writes != 0 {
		t.Fatalf("get output %s, writes %d", out.String(), writes)
	}
	c = newRepoSettingsCmd()
	c.SetArgs([]string{"update", "auto-decline", "-R", "settings.test/PRJ/repo", "--file", "-", "--yes"})
	c.SetIn(strings.NewReader(`{"enabled":false}`))
	c.SetOut(&out)
	c.SetErr(&out)
	if err := c.Execute(); err == nil || !strings.Contains(err.Error(), "admin permission required") {
		t.Fatalf("expected server error, got %v", err)
	}
	if writes != 1 {
		t.Fatalf("writes = %d, want 1", writes)
	}
}

func TestRepoSettingsCLIValidatesBeforeContextOrWrite(t *testing.T) {
	for _, args := range [][]string{
		{"update", "pull-requests", "--minimum-approvals", "-1"},
		{"update", "auto-decline", "--minimum-approvals", "2"},
		{"update", "pull-requests", "--minimum-approvals", "2", "--file", "unused"},
		{"update", "pull-requests", "--file", "-"},
		{"update", "pull-requests", "--file", "-", "--yes"},
		{"create", "default-reviewers"},
	} {
		c := newRepoSettingsCmd()
		c.SetArgs(args)
		c.SetIn(strings.NewReader(`null`))
		var out bytes.Buffer
		c.SetOut(&out)
		c.SetErr(&out)
		err := c.Execute()
		if err == nil || strings.Contains(err.Error(), "host") || strings.Contains(err.Error(), "git") {
			t.Errorf("args %v: expected input error before resolving context, got %v", args, err)
		}
	}
}
