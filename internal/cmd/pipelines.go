package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

func newPipelinesCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "pipelines",
		Aliases: []string{"pipe", "build", "builds"},
		Short:   "Work with Bitbucket Pipelines / build statuses",
	}
	c.AddCommand(newPipelinesListCmd())
	return c
}

func newPipelinesListCmd() *cobra.Command {
	var repoFlag, hostFlag, ref string
	var limit int
	c := &cobra.Command{
		Use:   "list",
		Short: "List recent build / pipeline runs",
		Long: `List recent builds for a ref.

Cloud: lists Bitbucket Pipelines runs for the repo, filtered by --ref if given.
Server: resolves --ref (default: repo's default branch) to a commit and lists
build statuses reported against that commit.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, project, slug, err := resolveContext(repoFlag, hostFlag)
			if err != nil {
				return err
			}
			builds, err := svc.ListBuildsForRef(project, slug, ref, limit)
			if err != nil {
				return err
			}
			if len(builds) == 0 {
				fmt.Println("No builds.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tSTATE\tNAME\tREF\tWHEN\tURL")
			for _, b := range builds {
				when := ""
				if !b.CreatedAt.IsZero() {
					when = humanTime(b.CreatedAt)
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					b.ID, styleState(b.State), truncate(b.Name, 40), b.Ref, when, b.URL,
				)
			}
			return w.Flush()
		},
	}
	c.Flags().StringVarP(&repoFlag, "repo", "R", "", "PROJ/repo or host/PROJ/repo")
	c.Flags().StringVar(&hostFlag, "host", "", "host")
	c.Flags().StringVar(&ref, "ref", "", "branch / ref (default: repo default branch)")
	c.Flags().IntVarP(&limit, "limit", "L", 25, "max builds to fetch")
	return c
}

func humanTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
