package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/hugs7/bitbucket-cli/internal/api"
	"github.com/hugs7/bitbucket-cli/internal/strutil"
)

// newPRStackCmd implements `bb pr stack` — groups open PRs into
// dependency chains (where one PR's source ref is another PR's
// target ref) and prints them as a tree. With --all, singleton
// "stacks" (standalone PRs) are included too.
func newPRStackCmd() *cobra.Command {
	var repoFlag, hostFlag, state string
	var limit int
	var all bool
	c := &cobra.Command{
		Use:   "stack",
		Short: "Show PRs grouped into stacks (chains where one PR targets another's branch)",
		Long: `Detects stacked PRs by walking the source / target dependency graph
and prints each chain as a tree, base → tip. By default only chains
of 2+ PRs are shown; use --all to include standalone PRs as well.

Examples:
  bb pr stack
  bb pr stack --state ALL --all`,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, project, slug, err := resolveContext(repoFlag, hostFlag)
			if err != nil {
				return err
			}
			prs, err := svc.ListPRs(project, slug, state, limit)
			if err != nil {
				return err
			}
			stacks := api.ComputeStacks(prs)
			printed := 0
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			for _, s := range stacks {
				if !all && !s.IsStacked() {
					continue
				}
				printStack(w, s)
				printed++
			}
			w.Flush()
			if printed == 0 {
				fmt.Println("No stacked PRs found. Try --all to include standalone PRs.")
			}
			return nil
		},
	}
	c.Flags().StringVarP(&repoFlag, "repo", "R", "", "PROJ/repo or host/PROJ/repo")
	c.Flags().StringVar(&hostFlag, "host", "", "host")
	c.Flags().StringVar(&state, "state", "OPEN", "PR state filter (OPEN / MERGED / DECLINED / ALL)")
	c.Flags().IntVar(&limit, "limit", 100, "max PRs to scan")
	c.Flags().BoolVar(&all, "all", false, "include standalone PRs (singleton stacks)")
	return c
}

// printStack renders one stack as a tree fragment. The tip is at the
// top so the order matches how reviewers think ("merge from the
// bottom up"); ascii box-drawing keeps it readable in any terminal.
func printStack(w *tabwriter.Writer, s api.PRStack) {
	fmt.Fprintf(w, "STACK · %d PR%s\t\t\t\t\n", len(s.Items), pluralS(len(s.Items)))
	for i := len(s.Items) - 1; i >= 0; i-- {
		p := s.Items[i]
		var marker string
		switch {
		case len(s.Items) == 1:
			marker = "● "
		case i == len(s.Items)-1:
			marker = "▲ " // tip
		case i == 0:
			marker = "▼ " // base
		default:
			marker = "│ " // middle
		}
		fmt.Fprintf(w, "  %s#%d\t%s\t%s → %s\t%s\n",
			marker, p.ID,
			styleState(p.State),
			p.SourceRef, p.TargetRef,
			strutil.Truncate(p.Title, 50),
		)
	}
	fmt.Fprintln(w, "\t\t\t\t")
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
