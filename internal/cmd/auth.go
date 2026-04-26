package cmd

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/hugs7/bitbucket-cli/internal/api"
	"github.com/hugs7/bitbucket-cli/internal/config"
)

func newAuthCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate bb with a Bitbucket host",
	}
	c.AddCommand(newAuthLoginCmd(), newAuthStatusCmd(), newAuthCheckCmd(), newAuthLogoutCmd())
	return c
}

func newAuthLoginCmd() *cobra.Command {
	var host string
	var hostType string
	var username string
	var token string

	c := &cobra.Command{
		Use:   "login",
		Short: "Log in to a Bitbucket host",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Step 1: ask for the host on its own so we can pick a smart
			// default for the host type (cloud for bitbucket.org, server for
			// anything else).
			if err := huh.NewForm(
				huh.NewGroup(
					huh.NewInput().
						Title("Bitbucket host").
						Description("Hostname only, e.g. bitbucket.org or bitbucket.mycorp.example").
						Placeholder("bitbucket.org").
						Value(&host),
				),
			).Run(); err != nil {
				return err
			}
			if host == "" {
				host = "bitbucket.org"
			}
			if hostType == "" {
				if host == "bitbucket.org" {
					hostType = "cloud"
				} else {
					hostType = "server"
				}
			}

			// Step 2: confirm host type, then collect username + token.
			if err := huh.NewForm(
				huh.NewGroup(
					huh.NewSelect[string]().
						Title("Host type").
						Options(
							huh.NewOption("Cloud (bitbucket.org)", "cloud"),
							huh.NewOption("Server / Data Center (self-hosted)", "server"),
						).
						Value(&hostType),
					huh.NewInput().
						Title("Username").
						Value(&username).
						Validate(func(s string) error {
							if s == "" {
								return fmt.Errorf("username is required")
							}
							return nil
						}),
					huh.NewInput().
						Title("App password / HTTP access token").
						EchoMode(huh.EchoModePassword).
						Value(&token).
						Validate(func(s string) error {
							if s == "" {
								return fmt.Errorf("token is required")
							}
							return nil
						}),
				),
			).Run(); err != nil {
				return err
			}

			h := config.Host{Type: hostType, Username: username, Token: token}
			if hostType == "server" {
				h.APIBase = fmt.Sprintf("https://%s/rest/api/1.0", host)
			}
			if err := config.SetHost(host, h); err != nil {
				return err
			}
			fmt.Printf("✓ Logged in to %s as %s\n", host, username)
			return nil
		},
	}

	c.Flags().StringVar(&host, "host", "", "Bitbucket host (e.g. bitbucket.org)")
	c.Flags().StringVar(&hostType, "type", "", "host type: cloud or server")
	c.Flags().StringVarP(&username, "username", "u", "", "username")
	c.Flags().StringVarP(&token, "token", "t", "", "app password / HTTP access token")
	return c
}

var (
	styleHost    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	styleDefault = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	styleLabel   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleOK      = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	styleErr     = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styleMuted   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

func maskToken(t string) string {
	if len(t) <= 4 {
		return "****"
	}
	return "••••••••" + t[len(t)-4:]
}

func newAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show authentication status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Get()
			if len(cfg.Hosts) == 0 {
				fmt.Println(styleMuted.Render("Not logged in to any host. Try `bb auth login`."))
				return nil
			}

			names := make([]string, 0, len(cfg.Hosts))
			for n := range cfg.Hosts {
				names = append(names, n)
			}
			sort.Strings(names)

			for i, name := range names {
				h := cfg.Hosts[name]
				header := styleHost.Render(name)
				if name == cfg.DefaultHost {
					header += "  " + styleDefault.Render("(default)")
				}
				fmt.Println(header)
				fmt.Printf("  %s %s\n", styleLabel.Render("Type:    "), h.Type)
				fmt.Printf("  %s %s\n", styleLabel.Render("User:    "), h.Username)
				fmt.Printf("  %s %s\n", styleLabel.Render("Token:   "), maskToken(h.Token))
				if h.APIBase != "" {
					fmt.Printf("  %s %s\n", styleLabel.Render("API base:"), h.APIBase)
				}
				if i < len(names)-1 {
					fmt.Println()
				}
			}
			return nil
		},
	}
}

// newAuthCheckCmd verifies stored credentials by hitting an authenticated
// "who am I" endpoint on each (or a single) host.
func newAuthCheckCmd() *cobra.Command {
	var hostFlag string
	c := &cobra.Command{
		Use:   "check",
		Short: "Verify stored credentials by calling the Bitbucket API",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Get()
			if len(cfg.Hosts) == 0 {
				return fmt.Errorf("not logged in to any host — run `bb auth login`")
			}

			targets := []string{}
			if hostFlag != "" {
				if _, ok := cfg.Hosts[hostFlag]; !ok {
					return fmt.Errorf("no auth for host %q", hostFlag)
				}
				targets = append(targets, hostFlag)
			} else {
				for n := range cfg.Hosts {
					targets = append(targets, n)
				}
				sort.Strings(targets)
			}

			anyFail := false
			for _, name := range targets {
				h := cfg.Hosts[name]
				if err := checkHost(h); err != nil {
					anyFail = true
					fmt.Printf("%s  %s — %s\n", styleErr.Render("✗"), styleHost.Render(name), err)
				} else {
					fmt.Printf("%s  %s — %s\n", styleOK.Render("✓"), styleHost.Render(name),
						styleMuted.Render("authenticated as "+h.Username))
				}
			}
			if anyFail {
				return fmt.Errorf("one or more hosts failed authentication")
			}
			return nil
		},
	}
	c.Flags().StringVar(&hostFlag, "host", "", "only check this host (default: all configured hosts)")
	return c
}

func checkHost(h config.Host) error {
	client := api.New("", h)

	// Use an auth-required endpoint on each. /projects is publicly readable
	// on many Server installs and gives false positives.
	endpoint := "user"
	if h.Type == "server" {
		endpoint = "profile/recent/repos?limit=1"
	}

	req, err := client.NewRequest("GET", endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return fmt.Errorf("HTTP %d — token rejected", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Best-effort: ignore body parse errors, the status code is the source of truth.
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return nil
}

func newAuthLogoutCmd() *cobra.Command {
	var host string
	c := &cobra.Command{
		Use:   "logout",
		Short: "Remove credentials for a host",
		RunE: func(cmd *cobra.Command, args []string) error {
			if host == "" {
				host = config.Get().DefaultHost
			}
			if host == "" {
				return fmt.Errorf("no host configured")
			}

			var confirm bool
			if err := huh.NewConfirm().
				Title(fmt.Sprintf("Remove credentials for %s?", host)).
				Value(&confirm).
				Run(); err != nil {
				return err
			}
			if !confirm {
				fmt.Println("Cancelled.")
				return nil
			}

			if err := config.RemoveHost(host); err != nil {
				return err
			}
			fmt.Printf("✓ Logged out of %s\n", host)
			return nil
		},
	}
	c.Flags().StringVar(&host, "host", "", "host to remove (default: current default host)")
	return c
}
