package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/hugs7/bitbucket-cli/internal/config"
)

func TestForceDeleteBranchesRestoresMatchingRestriction(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet:
			fmt.Fprint(w, `{"values":[{"id":42,"type":"no-deletes","matcher":{"id":"release/*-DO","type":{"id":"PATTERN"}},"users":[],"groups":[],"accessKeys":[]}]}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/branch-permissions/2.0/projects/PRJ/repos/repo/restrictions/42":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete:
			var body struct {
				Name string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Name == "" {
				t.Fatal("expected branch name")
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	client := New("example.test", config.Host{Type: "server", APIBase: server.URL + "/rest/api/1.0"})
	svc := &serverService{client: client, host: "example.test"}
	branches := []string{"release/R23E2-0-DO", "release/R23E3-0-DO"}
	if err := svc.ForceDeleteBranches("PRJ", "repo", branches); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"GET /rest/branch-permissions/2.0/projects/PRJ/repos/repo/restrictions",
		"DELETE /rest/branch-permissions/2.0/projects/PRJ/repos/repo/restrictions/42",
		"DELETE /rest/branch-utils/latest/projects/PRJ/repos/repo/branches",
		"DELETE /rest/branch-utils/latest/projects/PRJ/repos/repo/branches",
		"POST /rest/branch-permissions/2.0/projects/PRJ/repos/repo/restrictions",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}
