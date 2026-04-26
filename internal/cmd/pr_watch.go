package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/hugs7/bitbucket-cli/internal/api"
	"github.com/hugs7/bitbucket-cli/internal/strutil"
)

// newPRWatchCmd implements `bb pr watch <id>` — polls the PR's
// state and the build status of its source ref, printing a single
// line whenever something changes. Exits when the PR closes (merged
// or declined) or the latest build reaches a terminal state, unless
// --follow is set in which case it keeps polling forever.
func newPRWatchCmd() *cobra.Command {
	var repoFlag, hostFlag string
	var interval time.Duration
	var follow bool
	c := &cobra.Command{
		Use:   "watch <id>",
		Short: "Watch a PR's state and CI builds, printing updates as they change",
		Long: `Polls the given PR every --interval seconds and prints a line
each time its state, latest build, or approvals change. Exits when
the PR is merged/declined and the latest build is in a terminal state
(SUCCESSFUL / FAILED / STOPPED), or use --follow to poll indefinitely.

Examples:
  bb pr watch 123
  bb pr watch 123 --interval 30s --follow`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid PR id %q", args[0])
			}
			svc, project, slug, err := resolveContext(repoFlag, hostFlag)
			if err != nil {
				return err
			}
			if interval < time.Second {
				interval = time.Second
			}
			return runWatch(svc, project, slug, id, interval, follow)
		},
	}
	c.Flags().StringVarP(&repoFlag, "repo", "R", "", "PROJ/repo or host/PROJ/repo")
	c.Flags().StringVar(&hostFlag, "host", "", "host")
	c.Flags().DurationVar(&interval, "interval", 15*time.Second, "polling interval (min 1s)")
	c.Flags().BoolVarP(&follow, "follow", "f", false, "keep polling after terminal state is reached")
	return c
}

// watchSnapshot captures the parts of a PR's state we want to detect
// changes on. Building it cheaply per tick lets us diff against the
// previous tick to decide whether to print.
type watchSnapshot struct {
	state      string
	approvals  string // "alice✓ bob~ carol✗"
	buildState string
	buildName  string
	buildURL   string
}

// runWatch is the polling loop. Pulled out so tests / callers can
// drive it without re-doing the cobra wiring. It honours SIGINT so
// users get a graceful exit on Ctrl-C.
func runWatch(svc api.Service, project, slug string, id int, interval time.Duration, follow bool) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	var prev watchSnapshot
	first := true
	for {
		snap, pr, err := pollWatch(svc, project, slug, id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s ✗ poll: %v\n", time.Now().Format("15:04:05"), err)
		} else {
			if first || snap != prev {
				printWatchLine(snap, pr)
				prev = snap
				first = false
			}
			if !follow && watchTerminal(snap) {
				return nil
			}
		}

		select {
		case <-sigCh:
			fmt.Fprintln(os.Stderr, "\n✗ interrupted")
			return nil
		case <-time.After(interval):
		}
	}
}

// pollWatch fetches the PR + its latest build and packs the result
// into a watchSnapshot. The PR is also returned so the caller can
// pull the title/url for the first printed line.
func pollWatch(svc api.Service, project, slug string, id int) (watchSnapshot, *api.PullRequest, error) {
	pr, err := svc.GetPR(project, slug, id)
	if err != nil {
		return watchSnapshot{}, nil, err
	}
	snap := watchSnapshot{
		state:     pr.State,
		approvals: formatApprovals(pr),
	}
	if pr.SourceRef != "" {
		builds, berr := svc.ListBuildsForRef(project, slug, pr.SourceRef, 1)
		if berr == nil && len(builds) > 0 {
			snap.buildState = builds[0].State
			snap.buildName = builds[0].Name
			snap.buildURL = builds[0].URL
		}
	}
	return snap, pr, nil
}

// printWatchLine emits one human-readable status line for the
// snapshot. Coloured tags reuse styleState so the look matches `bb pr
// list` output.
func printWatchLine(s watchSnapshot, pr *api.PullRequest) {
	ts := time.Now().Format("15:04:05")
	build := "no build"
	if s.buildState != "" {
		build = s.buildState
		if s.buildName != "" {
			build += " (" + s.buildName + ")"
		}
	}
	approvals := s.approvals
	if approvals == "" {
		approvals = "no reviewers"
	}
	fmt.Printf("%s  PR #%d  %s  · %s  · %s  · %s\n",
		ts, pr.ID, styleState(s.state),
		strutil.Truncate(pr.Title, 50),
		build, approvals,
	)
	if s.buildURL != "" {
		fmt.Printf("           ↳ %s\n", s.buildURL)
	}
}

// watchTerminal returns true when neither the PR nor its build can
// progress further, so a non-follow watch should exit. We treat any
// non-OPEN PR state plus any non-INPROGRESS/PENDING build state as
// terminal.
func watchTerminal(s watchSnapshot) bool {
	if strings.EqualFold(s.state, "OPEN") {
		return false
	}
	switch strings.ToUpper(s.buildState) {
	case "", "INPROGRESS", "PENDING", "RUNNING":
		// No build, or still running — keep watching the PR for
		// merge/decline confirmation but don't exit yet.
		return s.buildState == ""
	}
	return true
}

// formatApprovals condenses the reviewer list into a one-line
// "alice✓ bob~ carol✗" summary so it fits on the watch line. The
// glyphs match bb's existing review status conventions: ✓ approved,
// ✗ needs work, ~ pending.
func formatApprovals(pr *api.PullRequest) string {
	if len(pr.Reviewers) == 0 {
		return ""
	}
	parts := make([]string, 0, len(pr.Reviewers))
	for _, r := range pr.Reviewers {
		name := r.Username
		if name == "" {
			name = r.DisplayName
		}
		switch strings.ToUpper(r.Status) {
		case "APPROVED":
			parts = append(parts, name+"✓")
		case "NEEDS_WORK":
			parts = append(parts, name+"✗")
		default:
			parts = append(parts, name+"~")
		}
	}
	return strings.Join(parts, " ")
}
