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

// User is a unified user representation returned from a directory
// search. The display name is what reviewers see in the Bitbucket UI;
// Username is what we send to AddReviewers / RemoveReviewers.
type User struct {
	Username    string // login (Server) or UUID/account_id (Cloud)
	DisplayName string
	Email       string
}

// Comment is a unified PR comment representation.
type Comment struct {
	ID        int
	Author    string
	Text      string
	CreatedAt time.Time
	UpdatedAt time.Time

	// Inline is set when the comment is anchored to a specific
	// file/line in the PR diff (vs a general PR-level comment).
	Inline *CommentInline
}

// CommentInline describes the file/line/side anchor of an inline review
// comment, mirroring the InlineCommentInput shape used to create them.
type CommentInline struct {
	Path string
	Line int
	Side string // "new" (added/RHS) or "old" (removed/LHS)
}

// Webhook represents a repository webhook subscription.
type Webhook struct {
	ID          string
	URL         string
	Description string
	Events      []string
	Active      bool
}

// WebhookInput is the payload for creating a new webhook.
type WebhookInput struct {
	URL         string
	Events      []string
	Active      bool
	Description string
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
