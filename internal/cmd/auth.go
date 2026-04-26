package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

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

	c := &cobra.Command{
		Use:   "login",
		Short: "Log in to a Bitbucket host",
		RunE: func(cmd *cobra.Command, args []string) error {
			reader := bufio.NewReader(os.Stdin)

			if host == "" {
				fmt.Print("Host (default: bitbucket.org): ")
				h, _ := reader.ReadString('\n')
				host = strings.TrimSpace(h)
				if host == "" {
					host = "bitbucket.org"
				}
			}
			if hostType == "" {
				if host == "bitbucket.org" {
					hostType = "cloud"
				} else {
					fmt.Print("Type [cloud|server] (default: server): ")
					t, _ := reader.ReadString('\n')
					hostType = strings.TrimSpace(t)
					if hostType == "" {
						hostType = "server"
					}
				}
			}

			fmt.Print("Username: ")
			user, _ := reader.ReadString('\n')
			user = strings.TrimSpace(user)

			fmt.Print("App password / access token: ")
			tok, _ := reader.ReadString('\n')
			tok = strings.TrimSpace(tok)

			h := config.Host{Type: hostType, Username: user, Token: tok}
			if hostType == "server" {
				h.APIBase = fmt.Sprintf("https://%s/rest/api/1.0", host)
			}
			if err := config.SetHost(host, h); err != nil {
				return err
			}
			fmt.Printf("✓ Logged in to %s as %s\n", host, user)
			return nil
		},
	}

	c.Flags().StringVar(&host, "host", "", "Bitbucket host (e.g. bitbucket.org or bitbucket.mycorp.example)")
	c.Flags().StringVar(&hostType, "type", "", "host type: cloud or server")
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
