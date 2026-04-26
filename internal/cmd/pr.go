package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/hugo/bb/internal/api"
)

func newPRCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "pr",
		Short: "Work with pull requests",
	}
	c.AddCommand(
		newPRListCmd(), newPRViewCmd(), newPRCreateCmd(),
		newPRCheckoutCmd(), newPRMergeCmd(), newPRDeclineCmd(),
		newPRDiffCmd(), newPRBrowseCmd(),
		newPREditCmd(), newPRApproveCmd(), newPRUnapproveCmd(), newPRNeedsWorkCmd(),
		newPRCommentCmd(),
	)
	return c
}

func newPRListCmd() *cobra.Command {
	var repoFlag, hostFlag, state string
	var limit int
	c := &cobra.Command{
		Use:   "list",
		Short: "List pull requests",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, project, slug, err := resolveContext(repoFlag, hostFlag)
			if err != nil {
				return err
			}
			prs, err := svc.ListPRs(project, slug, state, limit)
			if err != nil {
				return err
			}
			if len(prs) == 0 {
				fmt.Println("No pull requests.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tSTATE\tTITLE\tBRANCH\tAUTHOR")
			for _, p := range prs {
				fmt.Fprintf(w, "#%d\t%s\t%s\t%s → %s\t%s\n",
					p.ID, styleState(p.State),
					truncate(p.Title, 60),
					p.SourceRef, p.TargetRef, p.Author,
				)
			}
			return w.Flush()
		},
	}
	c.Flags().StringVarP(&repoFlag, "repo", "R", "", "PROJ/repo or host/PROJ/repo")
	c.Flags().StringVar(&hostFlag, "host", "", "host")
	c.Flags().StringVarP(&state, "state", "s", "OPEN", "OPEN, MERGED, DECLINED, ALL")
	c.Flags().IntVarP(&limit, "limit", "L", 25, "max PRs to fetch")
	return c
}

func newPRViewCmd() *cobra.Command {
	var repoFlag, hostFlag string
	c := &cobra.Command{
		Use:   "view <id>",
		Short: "View a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid PR id %q", args[0])
			}
			svc, project, slug, err := resolveContext(repoFlag, hostFlag)
			if err != nil {
				return err
			}
			p, err := svc.GetPR(project, slug, id)
			if err != nil {
				return err
			}
			w, done := withPager()
			defer done()
			b := lipgloss.NewStyle().Bold(true)
			muted := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
			branch := lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
			author := lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
			fmt.Fprintf(w, "%s  %s\n", b.Render(fmt.Sprintf("#%d", p.ID)), b.Render(p.Title))
			fmt.Fprintf(w, "%s · %s · %s → %s\n",
				styleState(p.State),
				author.Render(p.Author),
				branch.Render(p.SourceRef),
				branch.Render(p.TargetRef),
			)
			fmt.Fprintln(w, muted.Render(p.WebURL))
			if p.Description != "" {
				fmt.Fprintln(w)
				fmt.Fprintln(w, p.Description)
			}
			return nil
		},
	}
	c.Flags().StringVarP(&repoFlag, "repo", "R", "", "PROJ/repo or host/PROJ/repo")
	c.Flags().StringVar(&hostFlag, "host", "", "host")
	return c
}

func newPRCreateCmd() *cobra.Command {
	var repoFlag, hostFlag, title, body, source, target string
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a pull request",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, project, slug, err := resolveContext(repoFlag, hostFlag)
			if err != nil {
				return err
			}
			if source == "" {
				out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
				if err == nil {
					source = string(bytesTrim(out))
				}
			}
			if target == "" {
				r, err := svc.GetRepo(project, slug)
				if err == nil {
					target = r.DefaultRef
				}
			}

			form := huh.NewForm(huh.NewGroup(
				huh.NewInput().Title("Source branch").Value(&source).Validate(nonEmpty),
				huh.NewInput().Title("Target branch").Value(&target).Validate(nonEmpty),
				huh.NewInput().Title("Title").Value(&title).Validate(nonEmpty),
				huh.NewText().Title("Description (optional)").Value(&body),
			))
			if err := form.Run(); err != nil {
				return err
			}

			p, err := svc.CreatePR(project, slug, api.CreatePRInput{
				Title:       title,
				Description: body,
				SourceRef:   source,
				TargetRef:   target,
			})
			if err != nil {
				return err
			}
			fmt.Printf("✓ Created PR #%d: %s\n", p.ID, p.WebURL)
			return nil
		},
	}
	c.Flags().StringVarP(&repoFlag, "repo", "R", "", "PROJ/repo or host/PROJ/repo")
	c.Flags().StringVar(&hostFlag, "host", "", "host")
	c.Flags().StringVarP(&title, "title", "t", "", "PR title")
	c.Flags().StringVarP(&body, "body", "b", "", "PR description")
	c.Flags().StringVarP(&source, "source", "s", "", "source branch (default: current branch)")
	c.Flags().StringVarP(&target, "target", "T", "", "target branch (default: repo default branch)")
	return c
}

func newPRCheckoutCmd() *cobra.Command {
	var repoFlag, hostFlag string
	c := &cobra.Command{
		Use:   "checkout <id>",
		Short: "Check out a PR's source branch locally",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid PR id %q", args[0])
			}
			svc, project, slug, err := resolveContext(repoFlag, hostFlag)
			if err != nil {
				return err
			}
			p, err := svc.GetPR(project, slug, id)
			if err != nil {
				return err
			}
			fetch := exec.Command("git", "fetch", "origin", p.SourceRef)
			fetch.Stdout, fetch.Stderr = os.Stdout, os.Stderr
			if err := fetch.Run(); err != nil {
				return err
			}
			co := exec.Command("git", "checkout", p.SourceRef)
			co.Stdout, co.Stderr = os.Stdout, os.Stderr
			return co.Run()
		},
	}
	c.Flags().StringVarP(&repoFlag, "repo", "R", "", "PROJ/repo or host/PROJ/repo")
	c.Flags().StringVar(&hostFlag, "host", "", "host")
	return c
}

func newPRMergeCmd() *cobra.Command {
	var repoFlag, hostFlag string
	c := &cobra.Command{
		Use:   "merge <id>",
		Short: "Merge a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return err
			}
			svc, project, slug, err := resolveContext(repoFlag, hostFlag)
			if err != nil {
				return err
			}
			var confirm bool
			if err := huh.NewConfirm().
				Title(fmt.Sprintf("Merge PR #%d?", id)).
				Value(&confirm).Run(); err != nil {
				return err
			}
			if !confirm {
				return nil
			}
			if err := svc.MergePR(project, slug, id); err != nil {
				return err
			}
			fmt.Printf("✓ Merged PR #%d\n", id)
			return nil
		},
	}
	c.Flags().StringVarP(&repoFlag, "repo", "R", "", "PROJ/repo or host/PROJ/repo")
	c.Flags().StringVar(&hostFlag, "host", "", "host")
	return c
}

func newPRDeclineCmd() *cobra.Command {
	var repoFlag, hostFlag string
	c := &cobra.Command{
		Use:   "decline <id>",
		Short: "Decline a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return err
			}
			svc, project, slug, err := resolveContext(repoFlag, hostFlag)
			if err != nil {
				return err
			}
			if err := svc.DeclinePR(project, slug, id); err != nil {
				return err
			}
			fmt.Printf("✓ Declined PR #%d\n", id)
			return nil
		},
	}
	c.Flags().StringVarP(&repoFlag, "repo", "R", "", "PROJ/repo or host/PROJ/repo")
	c.Flags().StringVar(&hostFlag, "host", "", "host")
	return c
}

func newPRDiffCmd() *cobra.Command {
	var repoFlag, hostFlag string
	c := &cobra.Command{
		Use:   "diff <id>",
		Short: "Print the diff for a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return err
			}
			svc, project, slug, err := resolveContext(repoFlag, hostFlag)
			if err != nil {
				return err
			}
			d, err := svc.PRDiff(project, slug, id)
			if err != nil {
				return err
			}
			w, done := withPager()
			defer done()
			fmt.Fprint(w, colorizeDiff(d))
			return nil
		},
	}
	c.Flags().StringVarP(&repoFlag, "repo", "R", "", "PROJ/repo or host/PROJ/repo")
	c.Flags().StringVar(&hostFlag, "host", "", "host")
	return c
}

func newPRBrowseCmd() *cobra.Command {
	var repoFlag, hostFlag string
	c := &cobra.Command{
		Use:   "browse <id>",
		Short: "Open a pull request in your browser",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return err
			}
			svc, project, slug, err := resolveContext(repoFlag, hostFlag)
			if err != nil {
				return err
			}
			p, err := svc.GetPR(project, slug, id)
			if err != nil {
				return err
			}
			fmt.Println(p.WebURL)
			return openInBrowser(p.WebURL)
		},
	}
	c.Flags().StringVarP(&repoFlag, "repo", "R", "", "PROJ/repo or host/PROJ/repo")
	c.Flags().StringVar(&hostFlag, "host", "", "host")
	return c
}

func newPREditCmd() *cobra.Command {
	var repoFlag, hostFlag, body string
	c := &cobra.Command{
		Use:   "edit <id>",
		Short: "Edit a pull request's description in $EDITOR",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return err
			}
			svc, project, slug, err := resolveContext(repoFlag, hostFlag)
			if err != nil {
				return err
			}
			if body == "" {
				p, err := svc.GetPR(project, slug, id)
				if err != nil {
					return err
				}
				body, err = editText(p.Description, fmt.Sprintf("pr-%d-description", id))
				if err != nil {
					return err
				}
			}
			if err := svc.UpdatePRDescription(project, slug, id, body); err != nil {
				return err
			}
			fmt.Printf("✓ Updated description for PR #%d\n", id)
			return nil
		},
	}
	c.Flags().StringVarP(&repoFlag, "repo", "R", "", "PROJ/repo or host/PROJ/repo")
	c.Flags().StringVar(&hostFlag, "host", "", "host")
	c.Flags().StringVarP(&body, "body", "b", "", "new description (skips opening editor)")
	return c
}

func newPRApproveCmd() *cobra.Command {
	var repoFlag, hostFlag string
	c := &cobra.Command{
		Use:   "approve <id>",
		Short: "Approve a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return err
			}
			svc, project, slug, err := resolveContext(repoFlag, hostFlag)
			if err != nil {
				return err
			}
			if err := svc.ApprovePR(project, slug, id); err != nil {
				return err
			}
			fmt.Printf("✓ Approved PR #%d\n", id)
			return nil
		},
	}
	c.Flags().StringVarP(&repoFlag, "repo", "R", "", "PROJ/repo or host/PROJ/repo")
	c.Flags().StringVar(&hostFlag, "host", "", "host")
	return c
}

func newPRUnapproveCmd() *cobra.Command {
	var repoFlag, hostFlag string
	c := &cobra.Command{
		Use:   "unapprove <id>",
		Short: "Withdraw approval from a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return err
			}
			svc, project, slug, err := resolveContext(repoFlag, hostFlag)
			if err != nil {
				return err
			}
			if err := svc.UnapprovePR(project, slug, id); err != nil {
				return err
			}
			fmt.Printf("✓ Unapproved PR #%d\n", id)
			return nil
		},
	}
	c.Flags().StringVarP(&repoFlag, "repo", "R", "", "PROJ/repo or host/PROJ/repo")
	c.Flags().StringVar(&hostFlag, "host", "", "host")
	return c
}

func newPRNeedsWorkCmd() *cobra.Command {
	var repoFlag, hostFlag string
	c := &cobra.Command{
		Use:     "needs-work <id>",
		Aliases: []string{"request-changes"},
		Short:   "Mark a PR as needs work / request changes",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return err
			}
			svc, project, slug, err := resolveContext(repoFlag, hostFlag)
			if err != nil {
				return err
			}
			if err := svc.NeedsWorkPR(project, slug, id); err != nil {
				return err
			}
			fmt.Printf("✓ Marked PR #%d as needs work\n", id)
			return nil
		},
	}
	c.Flags().StringVarP(&repoFlag, "repo", "R", "", "PROJ/repo or host/PROJ/repo")
	c.Flags().StringVar(&hostFlag, "host", "", "host")
	return c
}

func newPRCommentCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "comment",
		Short: "Work with PR comments",
	}
	c.AddCommand(newPRCommentListCmd(), newPRCommentAddCmd())
	return c
}

func newPRCommentListCmd() *cobra.Command {
	var repoFlag, hostFlag string
	c := &cobra.Command{
		Use:   "list <id>",
		Short: "List comments on a PR",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return err
			}
			svc, project, slug, err := resolveContext(repoFlag, hostFlag)
			if err != nil {
				return err
			}
			comments, err := svc.ListComments(project, slug, id)
			if err != nil {
				return err
			}
			if len(comments) == 0 {
				fmt.Println("No comments.")
				return nil
			}
			w, done := withPager()
			defer done()
			b := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("13"))
			muted := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
			for i, c := range comments {
				if i > 0 {
					fmt.Fprintln(w)
				}
				when := ""
				if !c.CreatedAt.IsZero() {
					when = c.CreatedAt.Format("2006-01-02 15:04")
				}
				fmt.Fprintf(w, "%s  %s\n", b.Render(c.Author), muted.Render(when))
				fmt.Fprintln(w, c.Text)
			}
			return nil
		},
	}
	c.Flags().StringVarP(&repoFlag, "repo", "R", "", "PROJ/repo or host/PROJ/repo")
	c.Flags().StringVar(&hostFlag, "host", "", "host")
	return c
}

func newPRCommentAddCmd() *cobra.Command {
	var repoFlag, hostFlag, body string
	c := &cobra.Command{
		Use:   "add <id>",
		Short: "Add a comment to a PR",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return err
			}
			svc, project, slug, err := resolveContext(repoFlag, hostFlag)
			if err != nil {
				return err
			}
			if body == "" {
				body, err = editText("", fmt.Sprintf("pr-%d-comment", id))
				if err != nil {
					return err
				}
				body = strings.TrimSpace(body)
			}
			if body == "" {
				return fmt.Errorf("aborted: empty comment")
			}
			c, err := svc.AddComment(project, slug, id, body)
			if err != nil {
				return err
			}
			fmt.Printf("✓ Added comment #%d to PR #%d\n", c.ID, id)
			return nil
		},
	}
	c.Flags().StringVarP(&repoFlag, "repo", "R", "", "PROJ/repo or host/PROJ/repo")
	c.Flags().StringVar(&hostFlag, "host", "", "host")
	c.Flags().StringVarP(&body, "body", "b", "", "comment text (skips opening editor)")
	return c
}

func nonEmpty(s string) error {
	if s == "" {
		return fmt.Errorf("required")
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func bytesTrim(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == ' ') {
		b = b[:len(b)-1]
	}
	return b
}
