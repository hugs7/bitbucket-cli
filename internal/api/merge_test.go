package api

import (
	"errors"
	"strings"
	"testing"
)

type mergeServiceStub struct {
	pr             PullRequest
	openPRs        []PullRequest
	mergeErr       error
	listErr        error
	updateErrors   map[int]error
	getCalled      bool
	merged         bool
	listCalled     bool
	updatedTargets map[int]string
}

func (s *mergeServiceStub) GetPR(_, _ string, _ int) (*PullRequest, error) {
	s.getCalled = true
	return &s.pr, nil
}

func (s *mergeServiceStub) ListPRs(_, _, _ string, _ int) ([]PullRequest, error) {
	s.listCalled = true
	return s.openPRs, s.listErr
}

func (s *mergeServiceStub) MergePR(_, _ string, _ int, _ string) error {
	s.merged = true
	return s.mergeErr
}

func (s *mergeServiceStub) UpdatePRTarget(_, _ string, id int, targetRef string) error {
	if err := s.updateErrors[id]; err != nil {
		return err
	}
	if s.updatedTargets == nil {
		s.updatedTargets = make(map[int]string)
	}
	s.updatedTargets[id] = targetRef
	return nil
}

func TestMergePullRequestRetargetsDependents(t *testing.T) {
	svc := &mergeServiceStub{
		pr: PullRequest{ID: 1, SourceRef: "feature/base", TargetRef: "main"},
		openPRs: []PullRequest{
			{ID: 2, SourceRef: "feature/next", TargetRef: "feature/base"},
			{ID: 3, SourceRef: "feature/other", TargetRef: "main"},
		},
	}

	result, err := MergePullRequest(svc, "PROJ", "repo", 1, MergeOptions{RetargetDependents: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := svc.updatedTargets[2]; got != "main" {
		t.Fatalf("dependent target = %q, want main", got)
	}
	if _, ok := svc.updatedTargets[3]; ok {
		t.Fatal("unrelated PR was retargeted")
	}
	if len(result.Retargeted) != 1 || result.Retargeted[0].ID != 2 {
		t.Fatalf("retargeted = %#v, want PR #2", result.Retargeted)
	}
	if result.Retargeted[0].TargetRef != "main" {
		t.Fatalf("result target = %q, want main", result.Retargeted[0].TargetRef)
	}
}

func TestMergePullRequestCanSkipRetargeting(t *testing.T) {
	svc := &mergeServiceStub{pr: PullRequest{ID: 1, SourceRef: "feature/base", TargetRef: "main"}}

	if _, err := MergePullRequest(svc, "PROJ", "repo", 1, MergeOptions{}); err != nil {
		t.Fatal(err)
	}
	if !svc.merged {
		t.Fatal("PR was not merged")
	}
	if svc.getCalled {
		t.Fatal("PR details were fetched when retargeting was disabled")
	}
	if svc.listCalled {
		t.Fatal("dependent PRs were listed when retargeting was disabled")
	}
}

func TestMergePullRequestReportsPostMergeRetargetFailures(t *testing.T) {
	svc := &mergeServiceStub{
		pr: PullRequest{ID: 1, SourceRef: "feature/base", TargetRef: "main"},
		openPRs: []PullRequest{
			{ID: 2, SourceRef: "feature/next", TargetRef: "feature/base"},
			{ID: 3, SourceRef: "feature/last", TargetRef: "feature/base"},
		},
		updateErrors: map[int]error{2: errors.New("update rejected")},
	}

	result, err := MergePullRequest(svc, "PROJ", "repo", 1, MergeOptions{RetargetDependents: true})
	if err == nil || !strings.Contains(err.Error(), "merged PR #1") || !strings.Contains(err.Error(), "#2") {
		t.Fatalf("error = %v, want post-merge failure for PR #2", err)
	}
	if got := svc.updatedTargets[3]; got != "main" {
		t.Fatalf("second dependent target = %q, want main", got)
	}
	if len(result.Retargeted) != 1 || result.Retargeted[0].ID != 3 {
		t.Fatalf("retargeted = %#v, want PR #3", result.Retargeted)
	}
}

type bypassMergeStub struct{ mergeServiceStub }

func (*bypassMergeStub) MergePRBypassingChecks(_, _ string, _ int, _ string) error { return nil }

func TestIsBypassableApprovalVeto(t *testing.T) {
	veto := func(summary string) error {
		return errors.New(`merge PR 7: HTTP 409: {"errors":[{"message":"Merging the pull request has been vetoed.","exceptionName":"com.atlassian.bitbucket.pull.PullRequestMergeVetoedException","vetoes":[{"summaryMessage":"` + summary + `"}]}]}`)
	}
	approval := veto("Requires approvals")
	cases := []struct {
		name string
		svc  any
		err  error
		want bool
	}{
		{"approval veto", &bypassMergeStub{}, approval, true},
		{"required reviewers veto", &bypassMergeStub{}, veto("Not all required reviewers have approved yet"), true},
		{"build veto", &bypassMergeStub{}, veto("Not all required builds are successful yet"), false},
		{"merge conflict", &bypassMergeStub{}, errors.New(`HTTP 409: {"errors":[{"message":"The pull request has conflicts","conflicted":true}]}`), false},
		{"backend cannot bypass", &mergeServiceStub{}, approval, false},
		{"no error", &bypassMergeStub{}, nil, false},
	}
	for _, tc := range cases {
		if got := IsBypassableApprovalVeto(tc.svc, tc.err); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
