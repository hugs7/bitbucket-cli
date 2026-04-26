package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRepoCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "repo",
		Short: "Work with Bitbucket repositories",
	}
	c.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List repositories (not yet implemented)",
			RunE: func(cmd *cobra.Command, args []string) error {
				return fmt.Errorf("not yet implemented")
			},
		},
		&cobra.Command{
			Use:   "view [workspace/repo]",
			Short: "View a repository (not yet implemented)",
			RunE: func(cmd *cobra.Command, args []string) error {
				return fmt.Errorf("not yet implemented")
			},
		},
		&cobra.Command{
			Use:   "clone [workspace/repo]",
			Short: "Clone a repository (not yet implemented)",
			RunE: func(cmd *cobra.Command, args []string) error {
				return fmt.Errorf("not yet implemented")
			},
		},
	)
	return c
}
