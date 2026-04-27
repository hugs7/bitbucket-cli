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
		Use:   "bb [path]",
		Short: "bb is a command-line interface for Bitbucket",
		Long: `bb is a fast, comprehensive CLI for Bitbucket Cloud and Bitbucket Data Center.

With no arguments, opens the interactive home dashboard. Pass a
filesystem path (e.g. ` + "`bb .`" + ` or ` + "`bb ../other-repo`" + `) to skip the
dashboard and drop straight onto the repo overview TUI for the
working tree at that path.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.MaximumNArgs(1),
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return config.Load(cfgPath)
		},
		// Print the "a new release is available" nag *after* the
		// command has finished so a user running a long-running TUI
		// sees it on exit, not buried under the alt-screen output.
		// Subcommands inherit this from cobra unless they set their
		// own PersistentPostRun, which none of ours do.
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			notifyIfUpdateAvailable(info)
		},
		// `bb` with no args opens the interactive home dashboard. A
		// single positional path argument — `bb .`, `bb ../foo`,
		// `bb ~/code/bar` — short-circuits straight to the repo
		// overview TUI for that working tree, mirroring how `gh repo
		// view` accepts an optional path. The home loop only runs
		// when no path is given so the two flows stay independent.
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				return runRepoTUIAtPath(args[0])
			}
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
				case "repo":
					// Dashboard Enter on a recent-repo row drops us
					// onto the repo overview TUI (README + recent
					// PRs + builds). Re-using runRepoTUI keeps the
					// "p → PR TUI" follow-up loop identical to what
					// `bb repo` / `bb .` give the user.
					if err := runRepoTUI(svc, action.Project, action.Slug); err != nil {
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
