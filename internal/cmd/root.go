package cmd

import (
	"github.com/spf13/cobra"

	"github.com/hugs7/bitbucket-cli/internal/config"
	"github.com/hugs7/bitbucket-cli/internal/tui/home"
	"github.com/hugs7/bitbucket-cli/internal/tui/pr"
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
			var state *home.State
			for {
				action, next, err := home.Run(svc, state)
				if err != nil {
					return err
				}
				state = next // remember tab / search / cursors
				if action == nil {
					return nil
				}
				switch action.Kind {
				case "prs":
					if err := pr.Run(svc, action.Project, action.Slug); err != nil {
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
		newDotCmd(),
		newUpgradeCmd(info),
	)

	return root
}

// newDotCmd is the `bb .` shortcut: opens the interactive repo
// overview TUI for the current repository (auto-detected from cwd).
// Mirrors `bb repo` with no subcommand so users can flick between
// `bb`, `bb prs`, and `bb .` without typing more characters than
// necessary. Use `bb repo browse` for the older "open in browser"
// behaviour.
func newDotCmd() *cobra.Command {
	var repoFlag, hostFlag string
	c := &cobra.Command{
		Use:   ".",
		Short: "Open the current repository's interactive overview",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return openRepoTUI(repoFlag, hostFlag)
		},
	}
	c.Flags().StringVarP(&repoFlag, "repo", "R", "", "PROJ/repo or host/PROJ/repo")
	c.Flags().StringVar(&hostFlag, "host", "", "host (default: from git remote or configured default)")
	return c
}
