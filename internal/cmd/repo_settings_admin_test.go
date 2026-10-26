package cmd

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSettingsCLIHookEnableRequiresConfirmation(t *testing.T) {
	for _, answer := range []string{"no\n", "yes\n"} {
		t.Run(strings.TrimSpace(answer), func(t *testing.T) {
			writes := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writes++
				body, _ := io.ReadAll(r.Body)
				if r.Method != "PUT" || !strings.HasSuffix(r.URL.Path, "/settings/hooks/plugin:hook/enabled") || len(body) != 0 {
					t.Errorf("unexpected request %s %s body=%s", r.Method, r.URL, body)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			configureSettingsTestHost(t, server.URL)
			c := newRepoSettingsCmd()
			c.SetArgs([]string{"enable", "hooks", "--id", "plugin:hook", "-R", "settings.test/PRJ/repo"})
			c.SetIn(strings.NewReader(answer))
			var output bytes.Buffer
			c.SetOut(&output)
			c.SetErr(&output)
			err := c.Execute()
			if answer == "yes\n" {
				if err != nil || writes != 1 {
					t.Fatalf("writes=%d err=%v", writes, err)
				}
			} else if err == nil || writes != 0 {
				t.Fatalf("cancel must not write: writes=%d err=%v", writes, err)
			}
		})
	}
}

func TestSettingsCLIHookSecretsRequireExplicitReadAndHideWritePreview(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method == "GET" {
			fmt.Fprint(w, `{"credential":"existing-secret","count":1}`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if r.Method != "PUT" || !strings.Contains(string(body), `"credential":"new-secret"`) || !strings.Contains(string(body), `"count":1`) {
			t.Errorf("unexpected write %s %s", r.Method, body)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	configureSettingsTestHost(t, server.URL)
	c := newRepoSettingsCmd()
	c.SetArgs([]string{"get", "hook-settings", "--id", "plugin:hook", "-R", "settings.test/PRJ/repo"})
	var output bytes.Buffer
	c.SetOut(&output)
	c.SetErr(&output)
	if err := c.Execute(); err == nil || !strings.Contains(err.Error(), "--include-sensitive") || calls != 0 {
		t.Fatalf("implicit secret read: calls=%d err=%v", calls, err)
	}
	c = newRepoSettingsCmd()
	c.SetArgs([]string{"update", "hook-settings", "--id", "plugin:hook", "-R", "settings.test/PRJ/repo", "--file", "-", "--yes"})
	c.SetIn(strings.NewReader(`{"credential":"new-secret"}`))
	c.SetOut(&output)
	c.SetErr(&output)
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || strings.Contains(output.String(), "new-secret") || strings.Contains(output.String(), "existing-secret") {
		t.Fatalf("unexpected calls or leaked secret in confirmation: calls=%d output=%s", calls, &output)
	}
}
