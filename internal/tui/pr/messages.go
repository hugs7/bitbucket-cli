// Package pr — typed messages.
//
// Each async fetch and editor result lands as one of these so the
// Update switch can dispatch by type. Pulled out of pr.go to keep
// the model file shorter; everything is just data with no behaviour.
package pr

import "github.com/hugs7/bitbucket-cli/internal/api"

// ---------- messages ----------

type prsLoadedMsg struct{ prs []api.PullRequest }
type prBuildLoadedMsg struct {
	prID  int
	state string
}
type diffLoadedMsg struct {
	id   int
	diff string
}
type commentsLoadedMsg struct {
	id       int
	comments []api.Comment
}
type diffCommentsLoadedMsg struct {
	id       int
	comments []api.Comment
}
type actionDoneMsg struct {
	text string
	err  error
	// reload causes the PR list to refresh after the action.
	reload bool
}
type editorResultMsg struct {
	purpose   string // "edit-description" | "add-comment" | "reply-comment" | "edit-comment" | "add-inline-comment"
	prID      int
	commentID int // for reply-comment (parent) and edit-comment
	text      string
	err       error

	// inline-comment context (only set for "add-inline-comment")
	path string
	line int
	side string // "new" or "old"
}
type errMsg struct{ err error }

// aiDescribeDoneMsg lands when the configured AI command returns a
// suggested PR description. The TUI then opens the description editor
// pre-filled with `text` so the user can review / tweak before saving.
type aiDescribeDoneMsg struct {
	prID int
	text string
	err  error
}
