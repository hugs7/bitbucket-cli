package api

import (
	"testing"
	"time"
)

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

func TestSummarizeBuilds(t *testing.T) {
	at := func(minute int) time.Time { return time.Date(2026, 10, 9, 10, minute, 0, 0, time.UTC) }
	tests := []struct {
		name   string
		builds []Build
		want   string
	}{
		{"none", nil, ""},
		{"rerun passes after a failure", []Build{
			{ID: "plan-1", State: "FAILED", CreatedAt: at(1)},
			{ID: "plan-1", State: "SUCCESSFUL", CreatedAt: at(2)},
		}, "SUCCESSFUL"},
		{"older failure on another build still wins", []Build{
			{ID: "plan-2", State: "SUCCESSFUL", CreatedAt: at(3)},
			{ID: "plan-1", State: "FAILED", CreatedAt: at(1)},
		}, "FAILED"},
		{"running beats passing", []Build{
			{ID: "plan-1", State: "SUCCESSFUL", CreatedAt: at(1)},
			{ID: "plan-2", State: "INPROGRESS", CreatedAt: at(2)},
		}, "INPROGRESS"},
		{"snyk failure ignored", []Build{
			{ID: "plan-1", State: "SUCCESSFUL", CreatedAt: at(1)},
			{ID: "code/snyk (my-project)", Name: "code/snyk (my-project)", State: "FAILED", CreatedAt: at(2)},
		}, "SUCCESSFUL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SummarizeBuilds(tt.builds).State; got != tt.want {
				t.Fatalf("SummarizeBuilds() state = %q, want %q", got, tt.want)
			}
		})
	}
}
