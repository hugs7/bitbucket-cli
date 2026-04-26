package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/hugo/bb/internal/api"
	"github.com/hugo/bb/internal/config"
)

func newRepoCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "repo",
		Aliases: []string{"r"},
		Short:   "Work with Bitbucket repositories",
	}
	c.AddCommand(newRepoListCmd(), newRepoViewCmd(), newRepoCloneCmd(), newRepoBrowseCmd(), newRepoCreateCmd(), newRepoWebhookCmd())
	return c
}

func newRepoCreateCmd() *cobra.Command {
	var hostFlag, project, name, slug, description string
	var private bool
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a new repository",
		Long: `Create a new repository.

Server: --project is the project key.
Cloud:  --project is the workspace; --name becomes the slug if --slug is omitted.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				return fmt.Errorf("--project is required")
			}
			if name == "" {
				return fmt.Errorf("--name is required")
			}
			cfg := config.Get()
			host := hostFlag
			if host == "" {
				host = cfg.DefaultHost
			}
			hcfg, ok := cfg.Hosts[host]
			if !ok {
				return fmt.Errorf("no auth for host %q", host)
			}
			svc, err := api.NewService(host, hcfg)
			if err != nil {
				return err
			}
			r, err := svc.CreateRepo(api.CreateRepoInput{
				Project:     project,
				Slug:        slug,
				Name:        name,
				Description: description,
				Private:     private,
			})
			if err != nil {
				return err
			}
			fmt.Printf("✓ Created %s/%s\n", r.Project, r.Slug)
			if r.WebURL != "" {
				fmt.Println(r.WebURL)
			}
			if r.CloneHTTPS != "" {
				fmt.Println("clone (https):", r.CloneHTTPS)
			}
			return nil
		},
	}
	c.Flags().StringVar(&hostFlag, "host", "", "host (default: configured default)")
	c.Flags().StringVarP(&project, "project", "p", "", "project key (Server) or workspace (Cloud)")
	c.Flags().StringVarP(&name, "name", "n", "", "repository name")
	c.Flags().StringVar(&slug, "slug", "", "repository slug (Cloud; defaults to --name)")
	c.Flags().StringVarP(&description, "description", "d", "", "repository description")
	c.Flags().BoolVar(&private, "private", true, "private repo (Cloud only)")
	return c
}

func newRepoListCmd() *cobra.Command {
	var project, hostFlag string
	var limit int
	c := &cobra.Command{
		Use:   "list",
		Short: "List repositories in a project / workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				return fmt.Errorf("--project (workspace for cloud, project key for server) is required")
			}
			cfg := config.Get()
			host := hostFlag
			if host == "" {
				host = cfg.DefaultHost
			}
			hcfg, ok := cfg.Hosts[host]
			if !ok {
				return fmt.Errorf("no auth for host %q", host)
			}
			svc, err := api.NewService(host, hcfg)
			if err != nil {
				return err
			}
			repos, err := svc.ListRepos(project, limit)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "REPO\tDEFAULT\tDESCRIPTION")
			for _, r := range repos {
				desc := strings.SplitN(r.Description, "\n", 2)[0]
				if len(desc) > 60 {
					desc = desc[:57] + "..."
				}
				fmt.Fprintf(w, "%s\t%s\t%s\n",
					lipgloss.NewStyle().Bold(true).Render(r.Project+"/"+r.Slug),
					r.DefaultRef, desc,
				)
			}
			return w.Flush()
		},
	}
	c.Flags().StringVarP(&project, "project", "p", "", "project key (server) or workspace (cloud)")
	c.Flags().StringVar(&hostFlag, "host", "", "host (default: configured default)")
	c.Flags().IntVarP(&limit, "limit", "L", 25, "max repos to fetch")
	return c
}

func newRepoViewCmd() *cobra.Command {
	var repoFlag, hostFlag string
	c := &cobra.Command{
		Use:   "view",
		Short: "View details of a repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, project, slug, err := resolveContext(repoFlag, hostFlag)
			if err != nil {
				return err
			}
			r, err := svc.GetRepo(project, slug)
			if err != nil {
				return err
			}
			b := lipgloss.NewStyle().Bold(true)
			fmt.Println(b.Render(r.Project + "/" + r.Slug))
			if r.Description != "" {
				fmt.Println(r.Description)
			}
			fmt.Println()
			fmt.Printf("Default branch: %s\n", r.DefaultRef)
			fmt.Printf("Web URL:        %s\n", r.WebURL)
			fmt.Printf("Clone (HTTPS):  %s\n", r.CloneHTTPS)
			fmt.Printf("Clone (SSH):    %s\n", r.CloneSSH)
			return nil
		},
	}
	c.Flags().StringVarP(&repoFlag, "repo", "R", "", "PROJ/repo or host/PROJ/repo")
	c.Flags().StringVar(&hostFlag, "host", "", "host (default: from git remote or configured default)")
	return c
}

func newRepoCloneCmd() *cobra.Command {
	var hostFlag string
	var useSSH bool
	c := &cobra.Command{
		Use:   "clone <PROJ/repo> [-- <git-clone-args>...]",
		Short: "Clone a repository",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, project, slug, err := resolveContext(args[0], hostFlag)
			if err != nil {
				return err
			}
			r, err := svc.GetRepo(project, slug)
			if err != nil {
				return err
			}
			url := r.CloneHTTPS
			if useSSH && r.CloneSSH != "" {
				url = r.CloneSSH
			}
			if url == "" {
				return fmt.Errorf("no clone URL available")
			}
			gitArgs := append([]string{"clone", url}, args[1:]...)
			gc := exec.Command("git", gitArgs...)
			gc.Stdout, gc.Stderr, gc.Stdin = os.Stdout, os.Stderr, os.Stdin
			return gc.Run()
		},
	}
	c.Flags().StringVar(&hostFlag, "host", "", "host (default: configured default)")
	c.Flags().BoolVar(&useSSH, "ssh", false, "use the SSH clone URL")
	return c
}

func newRepoBrowseCmd() *cobra.Command {
	var repoFlag, hostFlag string
	c := &cobra.Command{
		Use:   "browse",
		Short: "Open the repository in your browser",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, project, slug, err := resolveContext(repoFlag, hostFlag)
			if err != nil {
				return err
			}
			r, err := svc.GetRepo(project, slug)
			if err != nil {
				return err
			}
			fmt.Println(r.WebURL)
			return openInBrowser(r.WebURL)
		},
	}
	c.Flags().StringVarP(&repoFlag, "repo", "R", "", "PROJ/repo or host/PROJ/repo")
	c.Flags().StringVar(&hostFlag, "host", "", "host (default: from git remote or configured default)")
	return c
}
