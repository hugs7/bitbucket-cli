package api

import "testing"

func TestWithoutSnykBuilds(t *testing.T) {
	got := WithoutSnykBuilds([]Build{
		{ID: "abc", Name: "code/snyk (my-project)", State: "SUCCESSFUL"},
		{ID: "def", Name: "Build my-app", State: "INPROGRESS"},
		{ID: "SNYK-KEY", Name: "", State: "FAILED"},
	})
	if len(got) != 1 || got[0].ID != "def" {
		t.Fatalf("expected only the app build, got %+v", got)
	}
}
