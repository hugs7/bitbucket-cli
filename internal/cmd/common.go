package cmd

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/hugs7/bitbucket-cli/internal/api"
	"github.com/hugs7/bitbucket-cli/internal/config"
	"github.com/hugs7/bitbucket-cli/internal/gitctx"
)

// resolveContext resolves which (host, project, slug) the command should
// operate on, given an optional --repo flag like "PROJ/repo" or
// "host/PROJ/repo". Falls back to the current git remote.
func resolveContext(repoFlag, hostFlag string) (api.Service, string, string, error) {
	cfg := config.Get()

	var host, project, slug string

	if repoFlag != "" {
		parts := strings.Split(repoFlag, "/")
		switch len(parts) {
		case 2:
			project, slug = parts[0], parts[1]
		case 3:
			host, project, slug = parts[0], parts[1], parts[2]
		default:
			return nil, "", "", fmt.Errorf("--repo must be PROJ/repo or host/PROJ/repo")
		}
	} else {
		r, err := gitctx.Current("")
		if err != nil {
			return nil, "", "", fmt.Errorf("not inside a git repo and --repo not given: %w", err)
		}
		host, project, slug = r.Host, r.Project, r.Slug
	}

	if hostFlag != "" {
		host = hostFlag
	}
	if host == "" {
		host = cfg.DefaultHost
	}
	if host == "" {
		return nil, "", "", fmt.Errorf("no host configured — run `bb auth login`")
	}
	hcfg, ok := cfg.Hosts[host]
	if !ok {
		return nil, "", "", fmt.Errorf("no auth for host %q — run `bb auth login --host %s`", host, host)
	}

	svc, err := api.NewService(host, hcfg)
	if err != nil {
		return nil, "", "", err
	}
	return svc, project, slug, nil
}

// openInBrowser opens a URL in the user's default browser.
func openInBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler"}
	default:
		cmd = "xdg-open"
	}
	args = append(args, url)
	return exec.Command(cmd, args...).Start()
}

// State styling shared across commands.
func styleState(s string) string {
	switch strings.ToUpper(s) {
	case "OPEN", "INPROGRESS", "PENDING":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(s) // yellow
	case "MERGED", "SUCCESSFUL", "SUCCESS":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(s) // green
	case "DECLINED", "FAILED", "CANCELLED", "STOPPED":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(s) // red
	default:
		return s
	}
}
