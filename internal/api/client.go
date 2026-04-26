// Package api is a thin HTTP client for the Bitbucket REST APIs.
//
// It supports both Bitbucket Cloud (api.bitbucket.org/2.0) and Bitbucket
// Data Center / Server (https://<host>/rest/api/1.0). Authentication uses
// HTTP Basic with the configured username + app password / HTTP access token.
package api

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hugs7/bitbucket-cli/internal/config"
)

type Client struct {
	host string
	cfg  config.Host
	http *http.Client
}

func New(host string, cfg config.Host) *Client {
	return &Client{
		host: host,
		cfg:  cfg,
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

// baseURL returns the API base for this host.
func (c *Client) baseURL() string {
	if c.cfg.APIBase != "" {
		return strings.TrimRight(c.cfg.APIBase, "/")
	}
	// Default for cloud.
	return "https://api.bitbucket.org/2.0"
}

// HostRoot returns the scheme+host portion of the configured API base
// (everything before "/rest/"). Useful for building URLs against
// sibling REST plugins like /rest/search/latest/.
func (c *Client) HostRoot() string {
	base := c.baseURL()
	if i := strings.Index(base, "/rest/"); i >= 0 {
		return base[:i]
	}
	return base
}

// NewRequest builds an authenticated request. endpoint may be a relative
// path or a full URL.
func (c *Client) NewRequest(method, endpoint string, body io.Reader) (*http.Request, error) {
	url := endpoint
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		url = c.baseURL() + "/" + strings.TrimLeft(endpoint, "/")
	}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	// Bitbucket Server / Data Center HTTP access tokens are Bearer tokens.
	// Bitbucket Cloud app passwords use HTTP Basic with the username.
	if c.cfg.Token != "" {
		switch c.cfg.Type {
		case "server":
			req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
		default:
			if c.cfg.Username != "" {
				req.SetBasicAuth(c.cfg.Username, c.cfg.Token)
			} else {
				req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
			}
		}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "bb-cli")
	return req, nil
}

func (c *Client) Do(req *http.Request) (*http.Response, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	return resp, nil
}
