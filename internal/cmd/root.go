package cmd

import (
	"github.com/spf13/cobra"

	"github.com/hugs7/bitbucket-cli/internal/config"
	"github.com/hugs7/bitbucket-cli/internal/tui"
)

type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

// NewRootCmd builds the top-level `bb` command tree.
func NewRootCmd(info BuildInfo) *cobra.Command {
	var cfgPath string

	root := &cobra.Command{
		Use:           "bb",
		Short:         "bb is a command-line interface for Bitbucket",
		Long:          "bb is a fast, comprehensive CLI for Bitbucket Cloud and Bitbucket Data Center.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return config.Load(cfgPath)
		},
		// `bb` with no args opens the interactive home dashboard. The
		// home model can return a "next action" (e.g. open the PR
		// browser for a particular repo); we loop until the user
		// quits cleanly so they can flow between TUIs without
		// dropping back to the shell each time.
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := defaultService("")
			if err != nil {
				return err
			}
			for {
				action, err := tui.Home(svc)
				if err != nil {
					return err
				}
				if action == nil {
					return nil
				}
				switch action.Kind {
				case "prs":
					if err := tui.PRs(svc, action.Project, action.Slug); err != nil {
						return err
					}
				}
			}
		},
	}

	root.PersistentFlags().StringVar(&cfgPath, "config", "", "path to config file (default: $XDG_CONFIG_HOME/bb/config.yml)")

	root.AddCommand(
		newVersionCmd(info),
		newAuthCmd(),
		newRepoCmd(),
		newPRCmd(),
		newPipelinesCmd(),
		newAPICmd(),
		newPRsCmd(),
	)

	return root
}
