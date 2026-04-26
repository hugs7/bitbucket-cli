package cmd

import (
	"github.com/spf13/cobra"

	"github.com/hugo/bb/internal/config"
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
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return config.Load(cfgPath)
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
