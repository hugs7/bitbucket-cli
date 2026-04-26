package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/hugs7/bitbucket-cli/internal/config"
)

// Service is the high-level API that bb commands talk to. Implementations
// translate between unified types and either Bitbucket Cloud (REST 2.0) or
// Bitbucket Server / Data Center (REST 1.0).
type Service interface {
	Host() string

	GetRepo(project, slug string) (*Repo, error)
	ListRepos(project string, limit int) ([]Repo, error)

	ListPRs(project, slug string, state string, limit int) ([]PullRequest, error)
	GetPR(project, slug string, id int) (*PullRequest, error)
	CreatePR(project, slug string, in CreatePRInput) (*PullRequest, error)
	MergePR(project, slug string, id int) error
	DeclinePR(project, slug string, id int) error
	PRDiff(project, slug string, id int) (string, error)

	UpdatePRDescription(project, slug string, id int, description string) error
	ApprovePR(project, slug string, id int) error
	UnapprovePR(project, slug string, id int) error
	NeedsWorkPR(project, slug string, id int) error

	ListComments(project, slug string, id int) ([]Comment, error)
	AddComment(project, slug string, id int, text string) (*Comment, error)
	AddInlineComment(project, slug string, prID int, in InlineCommentInput) (*Comment, error)
	EditComment(project, slug string, prID, commentID int, text string) (*Comment, error)
	DeleteComment(project, slug string, prID, commentID int) error
	ReplyComment(project, slug string, prID, parentID int, text string) (*Comment, error)

	AddReviewers(project, slug string, prID int, usernames []string) error
	RemoveReviewers(project, slug string, prID int, usernames []string) error

	CreateRepo(in CreateRepoInput) (*Repo, error)

	ListWebhooks(project, slug string) ([]Webhook, error)
	AddWebhook(project, slug string, in WebhookInput) (*Webhook, error)
	DeleteWebhook(project, slug, id string) error

	ListBuildsForRef(project, slug, ref string, limit int) ([]Build, error)
	TriggerPipeline(project, slug, ref string) (*Build, error)
	CancelPipeline(project, slug, idOrUUID string) error
	PipelineLogs(project, slug, idOrUUID string) (string, error)

	// ListMyReviewPRs returns open PRs the current authenticated user
	// is a reviewer on (Cloud uses participant filtering; Server uses
	// the inbox endpoint).
	ListMyReviewPRs(limit int) ([]ReviewPR, error)

	// GetReadme returns the rendered README markdown for a repo's
	// default branch. Returns ("", nil) if no README is found.
	GetReadme(project, slug string) (string, error)

	// SearchRepos performs a fuzzy search for repos by name across
	// projects (Server) or within the configured workspace (Cloud).
	// An empty query returns the most recently updated repos.
	SearchRepos(query string, limit int) ([]Repo, error)
}

// ReviewPR carries enough context to display and act on a PR pulled
// from the cross-repo "my reviews" feed.
type ReviewPR struct {
	PR      PullRequest
	Project string // workspace key (Cloud) or project key (Server)
	Slug    string // repo slug
}

// InlineCommentInput describes a file/line-anchored review comment.
//
// Side is "new" (added side / RHS, default) or "old" (removed side / LHS).
type InlineCommentInput struct {
	Text string
	Path string
	Line int
	Side string // "new" or "old"
}

type CreatePRInput struct {
	Title       string
	Description string
	SourceRef   string
	TargetRef   string
}

type CreateRepoInput struct {
	Project     string // workspace (Cloud) or project key (Server)
	Slug        string // optional for Cloud; required for Server
	Name        string
	Description string
	Private     bool
	SCM         string // defaults to "git"
}

// NewService picks the right implementation for a configured host.
func NewService(hostName string, h config.Host) (Service, error) {
	switch h.Type {
	case "cloud":
		return &cloudService{client: New(hostName, h), host: hostName}, nil
	case "server":
		return &serverService{client: New(hostName, h), host: hostName}, nil
	default:
		return nil, fmt.Errorf("unknown host type %q", h.Type)
	}
}

// ---------- helpers ----------

func decode(resp *http.Response, v any) error {
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if v == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func (c *Client) getJSON(endpoint string, v any) error {
	req, err := c.NewRequest("GET", endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	return decode(resp, v)
}

func (c *Client) postJSON(endpoint string, in, out any) error {
	return c.bodyJSON("POST", endpoint, in, out)
}

func (c *Client) putJSON(endpoint string, in, out any) error {
	return c.bodyJSON("PUT", endpoint, in, out)
}

func (c *Client) deleteJSON(endpoint string) error {
	req, err := c.NewRequest("DELETE", endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	return decode(resp, nil)
}

func (c *Client) bodyJSON(method, endpoint string, in, out any) error {
	body := strings.NewReader("")
	contentType := ""
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = strings.NewReader(string(b))
		contentType = "application/json"
	}
	req, err := c.NewRequest(method, endpoint, body)
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	return decode(resp, out)
}

// queryString joins a map of params into ?a=1&b=2 (sorted keys not
// guaranteed; fine for Bitbucket).
func queryString(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	v := url.Values{}
	for k, val := range params {
		if val == "" {
			continue
		}
		v.Set(k, val)
	}
	if len(v) == 0 {
		return ""
	}
	return "?" + v.Encode()
}

func itoa(i int) string { return strconv.Itoa(i) }
