package api

import (
	"fmt"
	"strings"
)

const dependentPRScanLimit = 100

// MergeOptions controls merge strategy and post-merge handling for stacked PRs.
type MergeOptions struct {
	StrategyID         string
	RetargetDependents bool
}

// MergeResult describes follow-up changes made after a successful merge.
type MergeResult struct {
	Retargeted []PullRequest
}

type mergeService interface {
	GetPR(project, slug string, id int) (*PullRequest, error)
	ListPRs(project, slug string, state string, limit int) ([]PullRequest, error)
	MergePR(project, slug string, id int, strategyID string) error
	UpdatePRTarget(project, slug string, id int, targetRef string) error
}

// MergePullRequest merges a PR and optionally moves open PRs targeting its
// source branch onto its target branch, preserving a stacked-PR chain after
// the intermediate branch has landed.
func MergePullRequest(
	svc mergeService,
	project, slug string,
	id int,
	options MergeOptions,
) (MergeResult, error) {
	var result MergeResult

	var pr *PullRequest
	if options.RetargetDependents {
		var err error
		pr, err = svc.GetPR(project, slug, id)
		if err != nil {
			return result, err
		}
	}
	if err := svc.MergePR(project, slug, id, options.StrategyID); err != nil {
		return result, err
	}
	if !options.RetargetDependents || pr == nil || pr.SourceRef == "" || pr.TargetRef == "" {
		return result, nil
	}

	prs, err := svc.ListPRs(project, slug, "OPEN", dependentPRScanLimit)
	if err != nil {
		return result, fmt.Errorf("merged PR #%d but failed to find dependent PRs: %w", id, err)
	}

	var failures []string
	for _, dependent := range prs {
		if dependent.TargetRef != pr.SourceRef {
			continue
		}
		if err := svc.UpdatePRTarget(project, slug, dependent.ID, pr.TargetRef); err != nil {
			failures = append(failures, fmt.Sprintf("#%d: %v", dependent.ID, err))
			continue
		}
		dependent.TargetRef = pr.TargetRef
		result.Retargeted = append(result.Retargeted, dependent)
	}
	if len(failures) > 0 {
		return result, fmt.Errorf(
			"merged PR #%d but failed to retarget dependent PR(s): %s",
			id,
			strings.Join(failures, "; "),
		)
	}

	return result, nil
}
