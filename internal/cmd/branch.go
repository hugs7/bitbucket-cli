package cmd

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

func newRepoBranchCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "branch",
		Aliases: []string{"branches"},
		Short:   "Manage repository branches",
	}
	c.AddCommand(newRepoBranchDeleteCmd())
	return c
}

func newRepoBranchDeleteCmd() *cobra.Command {
	var repoFlag, hostFlag string
	var force, yes bool
	c := &cobra.Command{
		Use:     "delete <branch>...",
		Aliases: []string{"rm", "remove"},
		Short:   "Delete remote repository branches",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, project, slug, err := resolveContext(repoFlag, hostFlag)
			if err != nil {
				return err
			}
			if !yes {
				var confirm bool
				if err := huh.NewConfirm().
					Title(fmt.Sprintf("Delete %d remote branch(es)?", len(args))).
					Value(&confirm).Run(); err != nil {
					return err
				}
				if !confirm {
					return nil
				}
			}
			if force {
				if err := svc.ForceDeleteBranches(project, slug, args); err != nil {
					return err
				}
			} else {
				for _, branch := range args {
					if err := svc.DeleteBranch(project, slug, branch); err != nil {
						return fmt.Errorf("delete branch %q: %w", branch, err)
					}
				}
			}
			for _, branch := range args {
				fmt.Printf("✓ Deleted branch %s\n", branch)
			}
			return nil
		},
	}
	c.Flags().StringVarP(&repoFlag, "repo", "R", "", "PROJ/repo or host/PROJ/repo")
	c.Flags().StringVar(&hostFlag, "host", "", "host (default: from git remote or configured default)")
	c.Flags().BoolVarP(&force, "force", "f", false, "temporarily remove and restore matching no-delete restrictions (Server only)")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation")
	return c
}
