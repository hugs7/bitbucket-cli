package cmd

import (
	"github.com/spf13/cobra"

	"github.com/hugs7/bitbucket-cli/internal/tui/pr"
)

// newPRsCmd is a top-level shortcut for the interactive PR browser.
func newPRsCmd() *cobra.Command {
	var repoFlag, hostFlag string
	c := &cobra.Command{
		Use:   "prs",
		Short: "Browse pull requests in an interactive TUI",
		Long: `Open an interactive Bubble Tea-based browser for pull requests.

Pass --repo / --host as with `+"`bb pr list`"+`, or run from inside a Bitbucket
clone to auto-detect.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, project, slug, err := resolveContext(repoFlag, hostFlag)
			if err != nil {
				return err
			}
			return pr.Run(svc, project, slug)
		},
	}
	c.Flags().StringVarP(&repoFlag, "repo", "R", "", "PROJ/repo or host/PROJ/repo")
	c.Flags().StringVar(&hostFlag, "host", "", "host (default: from git remote or configured default)")
	return c
}
