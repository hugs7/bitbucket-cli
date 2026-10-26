package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/hugs7/bitbucket-cli/internal/api"
	"github.com/hugs7/bitbucket-cli/internal/tui/home"
	"github.com/hugs7/bitbucket-cli/internal/tui/pr"
	"github.com/hugs7/bitbucket-cli/internal/tui/repo"
	"github.com/hugs7/bitbucket-cli/internal/tui/reposettings"
)

// runTUI owns navigation for every interactive entry point. Back follows the
// screen hierarchy even when its parent has not been opened yet; Quit leaves
// the entire session. Keep the resolved service when moving between screens,
// and preserve the home dashboard's tabs/search/cursors across round trips.
func runTUI(svc api.Service, screen, project, slug string, prID int) error {
	var state *home.State
	for {
		switch screen {
		case "home":
			action, next, err := home.Run(svc, state)
			if err != nil || action == nil {
				return err
			}
			state = next
			screen, project, slug = action.Kind, action.Project, action.Slug
		case "repo":
			action, err := repo.Repo(svc, project, slug)
			if err != nil || action == nil {
				return err
			}
			screen, project, slug = action.Kind, action.Project, action.Slug
		case "prs":
			back, err := pr.RunFocused(svc, project, slug, prID)
			if err != nil || !back {
				return err
			}
			prID = 0 // a pasted PR URL selects once, not on every later visit
			screen = "repo"
		case "settings":
			back, err := reposettings.Run(svc, project, slug)
			if err != nil || !back {
				return err
			}
			screen = "repo"
		default:
			return fmt.Errorf("unknown TUI screen %q", screen)
		}
	}
}

// newPRsCmd is a top-level shortcut for the interactive PR browser.
func newPRsCmd() *cobra.Command {
	var repoFlag, hostFlag string
	c := &cobra.Command{
		Use:   "prs",
		Short: "Browse pull requests in an interactive TUI",
		Long: `Open an interactive Bubble Tea-based browser for pull requests.

Pass --repo / --host as with ` + "`bb pr list`" + `, or run from inside a Bitbucket
clone to auto-detect.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, project, slug, err := resolveContext(repoFlag, hostFlag)
			if err != nil {
				return err
			}
			return runTUI(svc, "prs", project, slug, 0)
		},
	}
	c.Flags().StringVarP(&repoFlag, "repo", "R", "", "PROJ/repo or host/PROJ/repo")
	c.Flags().StringVar(&hostFlag, "host", "", "host (default: from git remote or configured default)")
	return c
}
