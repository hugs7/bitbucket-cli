// Package home — typed messages.
//
// Each loader, search tick, and read-me fetch sends one of these so
// the Update switch can dispatch by type instead of inspecting fields.
package home

import "github.com/hugs7/bitbucket-cli/internal/api"

type reviewsLoadedMsg struct{ prs []api.ReviewPR }
type authoredLoadedMsg struct{ prs []api.ReviewPR }
type closedLoadedMsg struct{ prs []api.ReviewPR }
type dashBuildLoadedMsg struct{ key, state string }
type recentReposLoadedMsg struct{ repos []api.Repo }
type reposLoadedMsg struct{ repos []api.Repo }
type readmeLoadedMsg struct {
	project, slug string
	body          string
}
type searchTickMsg struct {
	q       string
	version int
}
type homeErrMsg struct{ err error }

// homeMergeBlockedMsg reports a merge vetoed for missing approvals that
// can be retried with merge checks temporarily bypassed.
type homeMergeBlockedMsg struct {
	pr    api.ReviewPR
	label string
}
type homeActionDoneMsg struct {
	text string
	err  error
}
