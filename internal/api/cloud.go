package api

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// cloudService implements Service for Bitbucket Cloud (REST 2.0).
type cloudService struct {
	client *Client
	host   string
}

func (c *cloudService) Host() string { return c.host }

type clLinks struct {
	HTML  struct{ Href string `json:"href"` } `json:"html"`
	Clone []struct {
		Name string `json:"name"`
		Href string `json:"href"`
	} `json:"clone"`
}

type clRepo struct {
	Slug        string  `json:"slug"`
	FullName    string  `json:"full_name"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Mainbranch  struct{ Name string `json:"name"` } `json:"mainbranch"`
	Links       clLinks `json:"links"`
}

type clUser struct {
	DisplayName string `json:"display_name"`
	Nickname    string `json:"nickname"`
}

type clBranch struct {
	Branch struct{ Name string `json:"name"` } `json:"branch"`
}

type clPR struct {
	ID          int      `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"summary"`
	State       string   `json:"state"`
	CreatedOn   string   `json:"created_on"`
	UpdatedOn   string   `json:"updated_on"`
	Author      clUser   `json:"author"`
	Source      clBranch `json:"source"`
	Destination clBranch `json:"destination"`
	Links       clLinks  `json:"links"`
}

type clPaged[T any] struct {
	Values []T    `json:"values"`
	Next   string `json:"next"`
}

type clPipeline struct {
	UUID        string `json:"uuid"`
	BuildNumber int    `json:"build_number"`
	State       struct {
		Name   string `json:"name"`
		Result struct{ Name string `json:"name"` } `json:"result"`
	} `json:"state"`
	Target struct {
		RefName string `json:"ref_name"`
		Commit  struct{ Hash string `json:"hash"` } `json:"commit"`
	} `json:"target"`
	CreatedOn string `json:"created_on"`
	Links     clLinks `json:"links"`
}

func (r clRepo) toRepo() Repo {
	out := Repo{
		Slug:       r.Slug,
		Name:       r.Name,
		Description: r.Description,
		DefaultRef: r.Mainbranch.Name,
		WebURL:     r.Links.HTML.Href,
	}
	// FullName is "workspace/slug".
	if i := strings.Index(r.FullName, "/"); i > 0 {
		out.Project = r.FullName[:i]
	}
	for _, c := range r.Links.Clone {
		switch c.Name {
		case "https":
			out.CloneHTTPS = c.Href
		case "ssh":
			out.CloneSSH = c.Href
		}
	}
	return out
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func (p clPR) toPR() PullRequest {
	return PullRequest{
		ID:          p.ID,
		Title:       p.Title,
		Description: p.Description,
		State:       p.State,
		Author:      p.Author.DisplayName,
		SourceRef:   p.Source.Branch.Name,
		TargetRef:   p.Destination.Branch.Name,
		CreatedAt:   parseTime(p.CreatedOn),
		UpdatedAt:   parseTime(p.UpdatedOn),
		WebURL:      p.Links.HTML.Href,
	}
}

func (c *cloudService) GetRepo(workspace, slug string) (*Repo, error) {
	var r clRepo
	if err := c.client.getJSON(fmt.Sprintf("repositories/%s/%s", workspace, slug), &r); err != nil {
		return nil, err
	}
	out := r.toRepo()
	return &out, nil
}

func (c *cloudService) ListRepos(workspace string, limit int) ([]Repo, error) {
	if limit <= 0 {
		limit = 25
	}
	endpoint := fmt.Sprintf("repositories/%s%s", workspace, queryString(map[string]string{"pagelen": itoa(limit)}))
	var page clPaged[clRepo]
	if err := c.client.getJSON(endpoint, &page); err != nil {
		return nil, err
	}
	out := make([]Repo, 0, len(page.Values))
	for _, r := range page.Values {
		out = append(out, r.toRepo())
	}
	return out, nil
}

func (c *cloudService) ListPRs(workspace, slug, state string, limit int) ([]PullRequest, error) {
	if limit <= 0 {
		limit = 25
	}
	state = strings.ToUpper(state)
	if state == "" {
		state = "OPEN"
	}
	params := map[string]string{"pagelen": itoa(limit)}
	if state != "ALL" {
		params["state"] = state
	}
	endpoint := fmt.Sprintf("repositories/%s/%s/pullrequests%s", workspace, slug, queryString(params))
	var page clPaged[clPR]
	if err := c.client.getJSON(endpoint, &page); err != nil {
		return nil, err
	}
	out := make([]PullRequest, 0, len(page.Values))
	for _, p := range page.Values {
		out = append(out, p.toPR())
	}
	return out, nil
}

func (c *cloudService) GetPR(workspace, slug string, id int) (*PullRequest, error) {
	var p clPR
	if err := c.client.getJSON(fmt.Sprintf("repositories/%s/%s/pullrequests/%d", workspace, slug, id), &p); err != nil {
		return nil, err
	}
	out := p.toPR()
	return &out, nil
}

func (c *cloudService) CreatePR(workspace, slug string, in CreatePRInput) (*PullRequest, error) {
	body := map[string]any{
		"title":       in.Title,
		"description": in.Description,
		"source":      map[string]any{"branch": map[string]string{"name": in.SourceRef}},
		"destination": map[string]any{"branch": map[string]string{"name": in.TargetRef}},
	}
	var p clPR
	if err := c.client.postJSON(fmt.Sprintf("repositories/%s/%s/pullrequests", workspace, slug), body, &p); err != nil {
		return nil, err
	}
	out := p.toPR()
	return &out, nil
}

func (c *cloudService) MergePR(workspace, slug string, id int) error {
	return c.client.postJSON(fmt.Sprintf("repositories/%s/%s/pullrequests/%d/merge", workspace, slug, id), map[string]any{}, nil)
}

func (c *cloudService) DeclinePR(workspace, slug string, id int) error {
	return c.client.postJSON(fmt.Sprintf("repositories/%s/%s/pullrequests/%d/decline", workspace, slug, id), nil, nil)
}

func (c *cloudService) PRDiff(workspace, slug string, id int) (string, error) {
	req, err := c.client.NewRequest("GET", fmt.Sprintf("repositories/%s/%s/pullrequests/%d/diff", workspace, slug, id), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "text/plain")
	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// --- not-yet-implemented stubs for cloud (work for server today) ---

var errCloudTodo = fmt.Errorf("not yet implemented for Bitbucket Cloud — please open an issue")

func (c *cloudService) UpdatePRDescription(workspace, slug string, id int, description string) error {
	body := map[string]any{"description": description}
	return c.client.putJSON(fmt.Sprintf("repositories/%s/%s/pullrequests/%d", workspace, slug, id), body, nil)
}
func (c *cloudService) ApprovePR(workspace, slug string, id int) error {
	return c.client.postJSON(fmt.Sprintf("repositories/%s/%s/pullrequests/%d/approve", workspace, slug, id), nil, nil)
}
func (c *cloudService) UnapprovePR(workspace, slug string, id int) error {
	return c.client.deleteJSON(fmt.Sprintf("repositories/%s/%s/pullrequests/%d/approve", workspace, slug, id))
}
func (c *cloudService) NeedsWorkPR(workspace, slug string, id int) error {
	return c.client.postJSON(fmt.Sprintf("repositories/%s/%s/pullrequests/%d/request-changes", workspace, slug, id), nil, nil)
}
func (c *cloudService) ListComments(workspace, slug string, id int) ([]Comment, error) {
	type cc struct {
		ID      int    `json:"id"`
		Content struct{ Raw string `json:"raw"` } `json:"content"`
		User    clUser `json:"user"`
		Created string `json:"created_on"`
		Updated string `json:"updated_on"`
	}
	var page clPaged[cc]
	if err := c.client.getJSON(fmt.Sprintf("repositories/%s/%s/pullrequests/%d/comments?pagelen=100", workspace, slug, id), &page); err != nil {
		return nil, err
	}
	out := make([]Comment, 0, len(page.Values))
	for _, x := range page.Values {
		out = append(out, Comment{
			ID: x.ID, Author: x.User.DisplayName, Text: x.Content.Raw,
			CreatedAt: parseTime(x.Created), UpdatedAt: parseTime(x.Updated),
		})
	}
	return out, nil
}
func (c *cloudService) AddComment(workspace, slug string, id int, text string) (*Comment, error) {
	_ = errCloudTodo
	body := map[string]any{"content": map[string]string{"raw": text}}
	type cc struct {
		ID      int    `json:"id"`
		Content struct{ Raw string `json:"raw"` } `json:"content"`
		User    clUser `json:"user"`
		Created string `json:"created_on"`
		Updated string `json:"updated_on"`
	}
	var resp cc
	if err := c.client.postJSON(fmt.Sprintf("repositories/%s/%s/pullrequests/%d/comments", workspace, slug, id), body, &resp); err != nil {
		return nil, err
	}
	return &Comment{
		ID: resp.ID, Author: resp.User.DisplayName, Text: resp.Content.Raw,
		CreatedAt: parseTime(resp.Created), UpdatedAt: parseTime(resp.Updated),
	}, nil
}

func (c *cloudService) ListBuildsForRef(workspace, slug, ref string, limit int) ([]Build, error) {
	if limit <= 0 {
		limit = 25
	}
	// Cloud's pipelines endpoint isn't filterable by ref via a clean param;
	// we fetch the latest N and filter client-side if a ref was given.
	endpoint := fmt.Sprintf("repositories/%s/%s/pipelines/%s",
		workspace, slug,
		queryString(map[string]string{"sort": "-created_on", "pagelen": itoa(limit)}),
	)
	var page clPaged[clPipeline]
	if err := c.client.getJSON(endpoint, &page); err != nil {
		return nil, err
	}
	out := make([]Build, 0, len(page.Values))
	for _, p := range page.Values {
		if ref != "" && p.Target.RefName != ref {
			continue
		}
		state := p.State.Name
		if p.State.Result.Name != "" {
			state = p.State.Result.Name
		}
		out = append(out, Build{
			ID:        fmt.Sprintf("#%d", p.BuildNumber),
			Name:      p.UUID,
			State:     state,
			URL:       p.Links.HTML.Href,
			Ref:       p.Target.RefName,
			Commit:    p.Target.Commit.Hash,
			CreatedAt: parseTime(p.CreatedOn),
		})
	}
	return out, nil
}
