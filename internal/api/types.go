package api

import (
	"encoding/json"
	"strings"
	"time"
)

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
	ID        int
	Title     string
	State     string // OPEN / MERGED / DECLINED / SUPERSEDED
	Author    string
	SourceRef string
	// SourceCommit is the latest commit on the source branch, which is
	// the commit Bitbucket reports PR build status against.
	SourceCommit string
	TargetRef    string
	WebURL       string
	Description  string
	Reviewers    []Reviewer
	CreatedAt    time.Time
	UpdatedAt    time.Time
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
	ID          int    // numeric Server identity, used by default-reviewer rules
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

// Task represents an actionable "task" on a pull request. Bitbucket
// Server / Data Center 7+ models tasks as blocker comments: comments
// with severity=BLOCKER and a state of OPEN or RESOLVED. Cloud's
// task feature has been deprecated in favour of GFM checklists in the
// description, so the Cloud implementation returns no tasks.
type Task struct {
	ID     int
	Text   string
	Author string
	State  string // OPEN | RESOLVED
}

// MergeStrategy describes one merge mode the repo allows when
// merging a PR. ID is what MergePR's strategyID parameter expects;
// Name is the human label rendered in the confirm dialog. Default
// flags the repo's configured default so the UI can preselect it.
type MergeStrategy struct {
	ID      string
	Name    string
	Default bool
}

// RepoSettings is a best-effort snapshot of repository administration
// panels. Bitbucket Cloud and Server expose different settings through
// different APIs; unsupported or permission-denied panels are returned
// with Error populated so callers can show an inline status without
// failing the whole settings screen.
type RepoSettings struct {
	Panels map[string]RepoSettingsPanel
}

// RepoSettingsPanel contains display-ready rows for one settings panel.
// Name matches the TUI panel title (for example "Branch permissions").
type RepoSettingsPanel struct {
	Name  string
	Hint  string
	Items []RepoSettingsItem
	Error string
	// Raw retains reviewer conditions for the structured editor, not CLI dumps.
	Raw json.RawMessage `json:"-"`
}

// RepoSettingsItem is one row within a settings panel.
type RepoSettingsItem struct {
	Title  string
	Fields []RepoSettingsField
}

// RepoSettingsField is one key/value pair within a settings row.
type RepoSettingsField struct {
	Name  string
	Value string
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

// WithoutSnykBuilds drops Snyk security-scan statuses (e.g.
// "code/snyk (my-project)") so callers only see the real app builds.
func WithoutSnykBuilds(builds []Build) []Build {
	out := make([]Build, 0, len(builds))
	for _, b := range builds {
		if strings.Contains(strings.ToLower(b.Name+" "+b.ID), "snyk") {
			continue
		}
		out = append(out, b)
	}
	return out
}

// buildStateRank orders build states so the most severe one wins when
// several builds report on the same commit.
func buildStateRank(state string) int {
	switch strings.ToUpper(state) {
	case "FAILED", "STOPPED", "CANCELLED", "ERROR":
		return 3
	case "INPROGRESS", "IN_PROGRESS", "PENDING", "RUNNING":
		return 2
	case "SUCCESSFUL":
		return 1
	}
	return 0
}

// SummarizeBuilds reduces a commit's build statuses to the one that
// decides its overall state, the way Bitbucket's PR page does: Snyk
// scans are ignored, only the latest status per build key counts, and
// any failure outranks running builds, which outrank passing ones. It
// returns a zero Build when nothing is left.
func SummarizeBuilds(builds []Build) Build {
	latest := map[string]Build{}
	for _, b := range WithoutSnykBuilds(builds) {
		if prev, ok := latest[b.ID]; !ok || b.CreatedAt.After(prev.CreatedAt) {
			latest[b.ID] = b
		}
	}
	var worst Build
	for _, b := range latest {
		rank, worstRank := buildStateRank(b.State), buildStateRank(worst.State)
		if worst.ID == "" || rank > worstRank || (rank == worstRank && b.CreatedAt.After(worst.CreatedAt)) {
			worst = b
		}
	}
	return worst
}
