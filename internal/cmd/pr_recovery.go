package cmd

import (
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/hugs7/bitbucket-cli/internal/api"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

// newPRRecoverMergeChecksCmd restores a saved baseline without retrying a merge.
func newPRRecoverMergeChecksCmd() *cobra.Command {
	var repo, host string
	var yes bool
	c := &cobra.Command{
		Use: "recover-merge-checks", Short: "Restore settings saved by an interrupted admin merge",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, project, slug, err := resolveContext(repo, host)
			if err != nil {
				return err
			}
			recovery, ok := svc.(api.MergeCheckRecoveryService)
			if !ok {
				return fmt.Errorf("merge-check recovery is unsupported by this backend")
			}
			if !yes {
				if !isatty.IsTerminal(os.Stdin.Fd()) {
					return fmt.Errorf("pass --yes to restore the saved repository settings non-interactively")
				}
				var confirm bool
				if err := huh.NewConfirm().Title("Restore saved merge settings for " + project + "/" + slug + "?").
					Description("Restores the saved baseline, including reviewer rules. Coordinate any later admin edits first. This does not merge a PR.").Value(&confirm).Run(); err != nil {
					return err
				}
				if !confirm {
					return nil
				}
			}
			if err := recovery.RecoverMergeChecks(project, slug); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "✓ Restored and verified merge settings; no PR merge attempted")
			return nil
		},
	}
	c.Flags().StringVarP(&repo, "repo", "R", "", "PROJ/repo or host/PROJ/repo")
	c.Flags().StringVar(&host, "host", "", "host")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "confirm restoration of the saved baseline")
	return c
}
