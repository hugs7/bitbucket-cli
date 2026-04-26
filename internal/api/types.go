package api

import "time"

// Repo is the unified repository representation used by bb commands.
type Repo struct {
	Project     string
	Slug        string
	Name        string
	Description string
	WebURL      string
	CloneHTTPS  string
	CloneSSH    string
	DefaultRef  string
}

// PullRequest is the unified PR representation.
type PullRequest struct {
	ID          int
	Title       string
	State       string // OPEN / MERGED / DECLINED / SUPERSEDED
	Author      string
	SourceRef   string
	TargetRef   string
	WebURL      string
	Description string
	Reviewers   []Reviewer
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Reviewer represents a PR reviewer and their current review status.
type Reviewer struct {
	Username    string // login / slug
	DisplayName string
	Status      string // APPROVED / UNAPPROVED / NEEDS_WORK
	Approved    bool
}

// Comment is a unified PR comment representation.
type Comment struct {
	ID        int
	Author    string
	Text      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Build represents a build/pipeline run associated with a repo or commit.
type Build struct {
	ID        string
	Name      string
	State     string // SUCCESSFUL / INPROGRESS / FAILED / CANCELLED / PENDING
	URL       string
	Ref       string
	Commit    string
	CreatedAt time.Time
}
