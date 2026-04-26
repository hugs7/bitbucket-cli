package cmd

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hugo/bb/internal/api"
	"github.com/hugo/bb/internal/config"
)

func newAPICmd() *cobra.Command {
	var method string
	var hostFlag string

	c := &cobra.Command{
		Use:   "api <endpoint>",
		Short: "Make an authenticated request to the Bitbucket API",
		Long: `Make an authenticated HTTP request to the Bitbucket API.

The endpoint can be a path (e.g. "repositories/myws") which is appended to the
host's API base, or a full URL.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Get()
			hostName := hostFlag
			if hostName == "" {
				hostName = cfg.DefaultHost
			}
			host, ok := cfg.Hosts[hostName]
			if !ok {
				return fmt.Errorf("no auth for host %q — run `bb auth login`", hostName)
			}

			client := api.New(hostName, host)
			endpoint := args[0]

			req, err := client.NewRequest(strings.ToUpper(method), endpoint, nil)
			if err != nil {
				return err
			}
			resp, err := client.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			if _, err := io.Copy(os.Stdout, resp.Body); err != nil {
				return err
			}
			if resp.StatusCode >= 400 {
				return fmt.Errorf("HTTP %d", resp.StatusCode)
			}
			return nil
		},
	}

	c.Flags().StringVarP(&method, "method", "X", "GET", "HTTP method")
	c.Flags().StringVar(&hostFlag, "host", "", "host to query (default: configured default host)")
	_ = http.MethodGet // keep import stable
	return c
}
