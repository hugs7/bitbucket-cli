package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newPRCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "pr",
		Short: "Work with pull requests",
	}
	c.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List pull requests (not yet implemented)",
			RunE:  notImplemented,
		},
		&cobra.Command{
			Use:   "view [id]",
			Short: "View a pull request (not yet implemented)",
			RunE:  notImplemented,
		},
		&cobra.Command{
			Use:   "create",
			Short: "Create a pull request (not yet implemented)",
			RunE:  notImplemented,
		},
		&cobra.Command{
			Use:   "checkout [id]",
			Short: "Check out a pull request branch (not yet implemented)",
			RunE:  notImplemented,
		},
		&cobra.Command{
			Use:   "merge [id]",
			Short: "Merge a pull request (not yet implemented)",
			RunE:  notImplemented,
		},
	)
	return c
}

func notImplemented(cmd *cobra.Command, args []string) error {
	return fmt.Errorf("not yet implemented")
}
