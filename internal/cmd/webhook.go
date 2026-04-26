// Webhook subcommand for `bb repo webhook list/add/delete`. Event
// names differ between Bitbucket Server (e.g. repo:refs_changed,
// pr:opened) and Cloud (e.g. repo:push, pullrequest:created); pass the
// event names through verbatim — the API rejects anything unknown.
package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/hugs7/bitbucket-cli/internal/api"
)

func newRepoWebhookCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "webhook",
		Aliases: []string{"hook", "webhooks"},
		Short:   "Manage repository webhooks",
	}
	c.AddCommand(newWebhookListCmd(), newWebhookAddCmd(), newWebhookDeleteCmd())
	return c
}

func newWebhookListCmd() *cobra.Command {
	var repoFlag, hostFlag string
	c := &cobra.Command{
		Use:   "list",
		Short: "List webhooks on a repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, project, slug, err := resolveContext(repoFlag, hostFlag)
			if err != nil {
				return err
			}
			hooks, err := svc.ListWebhooks(project, slug)
			if err != nil {
				return err
			}
			if len(hooks) == 0 {
				fmt.Println("No webhooks.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tACTIVE\tURL\tEVENTS\tDESCRIPTION")
			for _, h := range hooks {
				active := "yes"
				if !h.Active {
					active = "no"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					lipgloss.NewStyle().Bold(true).Render(h.ID),
					active, h.URL, strings.Join(h.Events, ","), h.Description)
			}
			return w.Flush()
		},
	}
	c.Flags().StringVarP(&repoFlag, "repo", "R", "", "PROJ/repo or host/PROJ/repo")
	c.Flags().StringVar(&hostFlag, "host", "", "host (default: from git remote or configured default)")
	return c
}

func newWebhookAddCmd() *cobra.Command {
	var repoFlag, hostFlag, url, events, description string
	var active bool
	c := &cobra.Command{
		Use:   "add",
		Short: "Add a webhook to a repository",
		Long: `Add a webhook.

Event names differ between backends:
  Server: repo:refs_changed, pr:opened, pr:merged, pr:declined, pr:comment:added
  Cloud:  repo:push, pullrequest:created, pullrequest:fulfilled, pullrequest:rejected`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if url == "" {
				return fmt.Errorf("--url is required")
			}
			if events == "" {
				return fmt.Errorf("--events is required (comma-separated)")
			}
			svc, project, slug, err := resolveContext(repoFlag, hostFlag)
			if err != nil {
				return err
			}
			evList := strings.Split(events, ",")
			for i, e := range evList {
				evList[i] = strings.TrimSpace(e)
			}
			h, err := svc.AddWebhook(project, slug, api.WebhookInput{
				URL:         url,
				Events:      evList,
				Active:      active,
				Description: description,
			})
			if err != nil {
				return err
			}
			fmt.Printf("✓ Created webhook %s → %s\n", h.ID, h.URL)
			return nil
		},
	}
	c.Flags().StringVarP(&repoFlag, "repo", "R", "", "PROJ/repo or host/PROJ/repo")
	c.Flags().StringVar(&hostFlag, "host", "", "host (default: from git remote or configured default)")
	c.Flags().StringVar(&url, "url", "", "delivery URL (required)")
	c.Flags().StringVar(&events, "events", "", "comma-separated event names (required)")
	c.Flags().StringVarP(&description, "description", "d", "", "human-readable description")
	c.Flags().BoolVar(&active, "active", true, "whether the webhook is active")
	return c
}

func newWebhookDeleteCmd() *cobra.Command {
	var repoFlag, hostFlag string
	c := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a webhook by ID (Server) or UUID (Cloud)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, project, slug, err := resolveContext(repoFlag, hostFlag)
			if err != nil {
				return err
			}
			if err := svc.DeleteWebhook(project, slug, args[0]); err != nil {
				return err
			}
			fmt.Printf("✓ Deleted webhook %s\n", args[0])
			return nil
		},
	}
	c.Flags().StringVarP(&repoFlag, "repo", "R", "", "PROJ/repo or host/PROJ/repo")
	c.Flags().StringVar(&hostFlag, "host", "", "host (default: from git remote or configured default)")
	return c
}
