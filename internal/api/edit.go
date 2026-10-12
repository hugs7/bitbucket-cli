package api

import "strings"

// EditOptions lists the pull request fields to change. Empty Title and
// TargetRef, and a nil Description, leave that field as it is.
type EditOptions struct {
	Title       string
	Description *string
	// TargetRef is a branch name; a full "refs/heads/" ref is also accepted.
	TargetRef string
}

// EditResult describes what an edit changed.
type EditResult struct {
	// Updated names the changed fields in the order they were applied.
	Updated []string
	// AlreadyTargeted is set when the PR already targeted TargetRef, so no
	// retarget request was sent.
	AlreadyTargeted bool
}

type editService interface {
	GetPR(project, slug string, id int) (*PullRequest, error)
	UpdatePRTitle(project, slug string, id int, title string) error
	UpdatePRDescription(project, slug string, id int, description string) error
	UpdatePRTarget(project, slug string, id int, targetRef string) error
}

// EditPullRequest applies the requested target branch, title and description
// changes. A PR that already targets the requested branch is not retargeted,
// which skips Bitbucket Server's reviewer round-trip when nothing would change.
func EditPullRequest(
	svc editService,
	project, slug string,
	id int,
	options EditOptions,
) (EditResult, error) {
	var result EditResult

	if options.TargetRef != "" {
		target := strings.TrimPrefix(options.TargetRef, "refs/heads/")
		pr, err := svc.GetPR(project, slug, id)
		if err != nil {
			return result, err
		}
		if pr.TargetRef == target {
			result.AlreadyTargeted = true
		} else {
			if err := svc.UpdatePRTarget(project, slug, id, target); err != nil {
				return result, err
			}
			result.Updated = append(result.Updated, "target branch")
		}
	}

	if options.Title != "" {
		if err := svc.UpdatePRTitle(project, slug, id, options.Title); err != nil {
			return result, err
		}
		result.Updated = append(result.Updated, "title")
	}

	if options.Description != nil {
		if err := svc.UpdatePRDescription(project, slug, id, *options.Description); err != nil {
			return result, err
		}
		result.Updated = append(result.Updated, "description")
	}

	return result, nil
}
