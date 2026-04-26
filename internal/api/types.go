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
	CreatedAt   time.Time
	UpdatedAt   time.Time
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
