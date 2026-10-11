package api

import (
	"reflect"
	"testing"
)

type editServiceStub struct {
	pr           PullRequest
	targets      []string
	titles       []string
	descriptions []string
}

func (s *editServiceStub) GetPR(_, _ string, _ int) (*PullRequest, error) {
	return &s.pr, nil
}

func (s *editServiceStub) UpdatePRTitle(_, _ string, _ int, title string) error {
	s.titles = append(s.titles, title)
	return nil
}

func (s *editServiceStub) UpdatePRDescription(_, _ string, _ int, description string) error {
	s.descriptions = append(s.descriptions, description)
	return nil
}

func (s *editServiceStub) UpdatePRTarget(_, _ string, _ int, targetRef string) error {
	s.targets = append(s.targets, targetRef)
	return nil
}

func TestEditPullRequestRetargetsOnlyTheTarget(t *testing.T) {
	svc := &editServiceStub{pr: PullRequest{ID: 88, TargetRef: "master"}}

	result, err := EditPullRequest(svc, "WDP", "repo", 88, EditOptions{
		TargetRef: "refs/heads/release/R26M4-2-ST",
	})
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"release/R26M4-2-ST"}; !reflect.DeepEqual(svc.targets, want) {
		t.Fatalf("retargeted to %v, want %v", svc.targets, want)
	}
	if len(svc.titles) != 0 || len(svc.descriptions) != 0 {
		t.Fatalf("title %v / description %v changed, want neither", svc.titles, svc.descriptions)
	}
	if want := []string{"target branch"}; !reflect.DeepEqual(result.Updated, want) {
		t.Fatalf("updated %v, want %v", result.Updated, want)
	}
}

func TestEditPullRequestSkipsAnUnchangedTarget(t *testing.T) {
	svc := &editServiceStub{pr: PullRequest{ID: 7, TargetRef: "release/next"}}

	result, err := EditPullRequest(svc, "WDP", "repo", 7, EditOptions{
		TargetRef: "release/next",
		Title:     "New title",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(svc.targets) != 0 {
		t.Fatalf("retargeted to %v, want no request", svc.targets)
	}
	if !result.AlreadyTargeted {
		t.Fatal("AlreadyTargeted = false, want true")
	}
	if want := []string{"title"}; !reflect.DeepEqual(result.Updated, want) {
		t.Fatalf("updated %v, want %v", result.Updated, want)
	}
}

func TestEditPullRequestCanClearTheDescription(t *testing.T) {
	svc := &editServiceStub{pr: PullRequest{ID: 3, TargetRef: "main"}}
	empty := ""

	if _, err := EditPullRequest(svc, "WDP", "repo", 3, EditOptions{Description: &empty}); err != nil {
		t.Fatal(err)
	}

	if want := []string{""}; !reflect.DeepEqual(svc.descriptions, want) {
		t.Fatalf("descriptions %q, want %q", svc.descriptions, want)
	}
}
