package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hugs7/bitbucket-cli/internal/config"
)

func TestServerListPRBuildsReturnsOnlyLatestCommit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/ui/latest/projects/PRJ/repos/repo/pull-requests/7/builds" {
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"page":{"isLastPage":true,"values":[
			{"key":"PLAN-BR","state":"SUCCESSFUL","name":"branch build","dateAdded":2000,"ref":"refs/heads/feature","commit":{"id":"head"}},
			{"key":"PLAN-BR","state":"FAILED","name":"branch build","dateAdded":1000,"ref":"refs/heads/feature","commit":{"id":"older"}}
		]}}`)
	}))
	t.Cleanup(server.Close)

	client := New("example.test", config.Host{Type: "server", APIBase: server.URL + "/rest/api/1.0"})
	svc := &serverService{client: client, host: "example.test"}
	builds, err := svc.ListPRBuilds("PRJ", "repo", PullRequest{ID: 7, SourceCommit: "head"})
	if err != nil {
		t.Fatal(err)
	}
	if len(builds) != 1 || builds[0].Commit != "head" || builds[0].State != "SUCCESSFUL" {
		t.Fatalf("builds = %+v, want only the head commit's status", builds)
	}
}
