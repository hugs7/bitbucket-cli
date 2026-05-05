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

// clearStatusMsg fires after a transient toast's lifetime expires.
// gen is matched against the model's current statusGen so a newer
// toast set in the meantime isn't wiped by an older tick.
type clearStatusMsg struct{ gen int }

// aiDescribeDoneMsg lands when the configured AI command returns a
// suggested PR description. The TUI then opens the description editor
// pre-filled with `text` so the user can review / tweak before saving.
type aiDescribeDoneMsg struct {
	prID int
	text string
	err  error
}

// mergeStrategiesLoadedMsg lands after the M-key handler has fetched
// the repo's allowed merge strategies. The Update handler stages the
// strategy list, picks the default index, and switches into the
// merge-confirm view.
type mergeStrategiesLoadedMsg struct {
	prID       int
	sourceRef  string
	strategies []api.MergeStrategy
	err        error
}

// mergeTasksLoadedMsg lands after the merge confirm view kicks off
// its background task fetch. It just refreshes the open-tasks count
// shown in the dialog — the merge confirm view stays visible the
// whole time so users can still y/n without waiting.
type mergeTasksLoadedMsg struct {
	prID  int
	tasks []api.Task
	err   error
}
