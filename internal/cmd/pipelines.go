package cmd

import (
	"github.com/spf13/cobra"
)

func newPipelinesCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "pipelines",
		Aliases: []string{"pipe", "build", "builds"},
		Short:   "Work with Bitbucket Pipelines builds",
	}
	c.AddCommand(
		&cobra.Command{Use: "list", Short: "List recent pipeline runs (not yet implemented)", RunE: notImplemented},
		&cobra.Command{Use: "view [id]", Short: "View a pipeline run (not yet implemented)", RunE: notImplemented},
		&cobra.Command{Use: "logs [id]", Short: "Stream logs for a pipeline run (not yet implemented)", RunE: notImplemented},
		&cobra.Command{Use: "run", Short: "Trigger a pipeline run (not yet implemented)", RunE: notImplemented},
		&cobra.Command{Use: "cancel [id]", Short: "Cancel a pipeline run (not yet implemented)", RunE: notImplemented},
	)
	return c
}
