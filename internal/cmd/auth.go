package cmd

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/hugo/bb/internal/config"
)

func newAuthCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate bb with a Bitbucket host",
	}
	c.AddCommand(newAuthLoginCmd(), newAuthStatusCmd(), newAuthLogoutCmd())
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
			// Build a single form. Each field shows previous flag values as
			// the default, so flags + interactive can be mixed.
			if host == "" {
				host = "bitbucket.org"
			}
			if hostType == "" {
				hostType = "cloud"
			}

			form := huh.NewForm(
				huh.NewGroup(
					huh.NewInput().
						Title("Bitbucket host").
						Description("Hostname only, e.g. bitbucket.org or bitbucket.mycorp.example").
						Value(&host).
						Validate(func(s string) error {
							if s == "" {
								return fmt.Errorf("host is required")
							}
							return nil
						}),
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
			)

			if err := form.Run(); err != nil {
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

func newAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show authentication status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Get()
			if len(cfg.Hosts) == 0 {
				fmt.Println("Not logged in to any host. Try `bb auth login`.")
				return nil
			}
			for name, h := range cfg.Hosts {
				marker := " "
				if name == cfg.DefaultHost {
					marker = "*"
				}
				fmt.Printf("%s %s (%s) — user: %s\n", marker, name, h.Type, h.Username)
			}
			return nil
		},
	}
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
