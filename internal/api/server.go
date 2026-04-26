package api

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// serverService implements Service for Bitbucket Data Center / Server
// using the REST 1.0 API.
type serverService struct {
	client *Client
	host   string
}

func (s *serverService) Host() string { return s.host }

// --- raw response shapes (only the fields we need) ---

type srvLink struct{ Href string `json:"href"` }
type srvLinks struct {
	Self  []srvLink `json:"self"`
	Clone []struct {
		Href string `json:"href"`
		Name string `json:"name"`
	} `json:"clone"`
}
type srvRepo struct {
	Slug          string   `json:"slug"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Project       struct{ Key string } `json:"project"`
	Links         srvLinks `json:"links"`
	DefaultBranch string   `json:"defaultBranch"`
}

type srvUser struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

type srvRef struct {
	ID          string `json:"id"`
	DisplayID   string `json:"displayId"`
}

type srvPR struct {
	ID          int     `json:"id"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	State       string  `json:"state"`
	CreatedDate int64   `json:"createdDate"`
	UpdatedDate int64   `json:"updatedDate"`
	FromRef     srvRef  `json:"fromRef"`
	ToRef       srvRef  `json:"toRef"`
	Author      struct{ User srvUser } `json:"author"`
	Links       srvLinks `json:"links"`
}

type srvPaged[T any] struct {
	Size       int  `json:"size"`
	Limit      int  `json:"limit"`
	IsLastPage bool `json:"isLastPage"`
	Values     []T  `json:"values"`
}

type srvBuild struct {
	State          string `json:"state"`
	Key            string `json:"key"`
	Name           string `json:"name"`
	URL            string `json:"url"`
	DateAdded      int64  `json:"dateAdded"`
}

// --- conversions ---

func (r srvRepo) toRepo() Repo {
	out := Repo{
		Project:    r.Project.Key,
		Slug:       r.Slug,
		Name:       r.Name,
		Description: r.Description,
		DefaultRef: r.DefaultBranch,
	}
	for _, c := range r.Links.Clone {
		switch c.Name {
		case "http", "https":
			out.CloneHTTPS = c.Href
		case "ssh":
			out.CloneSSH = c.Href
		}
	}
	if len(r.Links.Self) > 0 {
		out.WebURL = r.Links.Self[0].Href
	}
	return out
}

func (p srvPR) toPR() PullRequest {
	out := PullRequest{
		ID:          p.ID,
		Title:       p.Title,
		Description: p.Description,
		State:       p.State,
		Author:      p.Author.User.DisplayName,
		SourceRef:   p.FromRef.DisplayID,
		TargetRef:   p.ToRef.DisplayID,
		CreatedAt:   time.UnixMilli(p.CreatedDate),
		UpdatedAt:   time.UnixMilli(p.UpdatedDate),
	}
	if len(p.Links.Self) > 0 {
		out.WebURL = p.Links.Self[0].Href
	}
	return out
}

// --- methods ---

func (s *serverService) GetRepo(project, slug string) (*Repo, error) {
	var r srvRepo
	if err := s.client.getJSON(fmt.Sprintf("projects/%s/repos/%s", project, slug), &r); err != nil {
		return nil, err
	}
	out := r.toRepo()
	return &out, nil
}

func (s *serverService) ListRepos(project string, limit int) ([]Repo, error) {
	if limit <= 0 {
		limit = 25
	}
	var page srvPaged[srvRepo]
	endpoint := fmt.Sprintf("projects/%s/repos%s", project, queryString(map[string]string{"limit": itoa(limit)}))
	if err := s.client.getJSON(endpoint, &page); err != nil {
		return nil, err
	}
	out := make([]Repo, 0, len(page.Values))
	for _, r := range page.Values {
		out = append(out, r.toRepo())
	}
	return out, nil
}

func (s *serverService) ListPRs(project, slug, state string, limit int) ([]PullRequest, error) {
	if limit <= 0 {
		limit = 25
	}
	state = strings.ToUpper(state)
	if state == "" {
		state = "OPEN"
	}
	if state == "ALL" {
		state = ""
	}
	endpoint := fmt.Sprintf("projects/%s/repos/%s/pull-requests%s",
		project, slug,
		queryString(map[string]string{"state": state, "limit": itoa(limit)}),
	)
	var page srvPaged[srvPR]
	if err := s.client.getJSON(endpoint, &page); err != nil {
		return nil, err
	}
	out := make([]PullRequest, 0, len(page.Values))
	for _, p := range page.Values {
		out = append(out, p.toPR())
	}
	return out, nil
}

func (s *serverService) GetPR(project, slug string, id int) (*PullRequest, error) {
	var p srvPR
	if err := s.client.getJSON(fmt.Sprintf("projects/%s/repos/%s/pull-requests/%d", project, slug, id), &p); err != nil {
		return nil, err
	}
	out := p.toPR()
	return &out, nil
}

func (s *serverService) CreatePR(project, slug string, in CreatePRInput) (*PullRequest, error) {
	body := map[string]any{
		"title":       in.Title,
		"description": in.Description,
		"state":       "OPEN",
		"open":        true,
		"closed":      false,
		"fromRef": map[string]any{
			"id":         "refs/heads/" + in.SourceRef,
			"repository": map[string]any{"slug": slug, "project": map[string]string{"key": project}},
		},
		"toRef": map[string]any{
			"id":         "refs/heads/" + in.TargetRef,
			"repository": map[string]any{"slug": slug, "project": map[string]string{"key": project}},
		},
	}
	var p srvPR
	if err := s.client.postJSON(fmt.Sprintf("projects/%s/repos/%s/pull-requests", project, slug), body, &p); err != nil {
		return nil, err
	}
	out := p.toPR()
	return &out, nil
}

func (s *serverService) MergePR(project, slug string, id int) error {
	pr, err := s.GetPR(project, slug, id)
	if err != nil {
		return err
	}
	// Server requires the PR's current version to merge — fetch raw to grab it.
	var raw struct{ Version int `json:"version"` }
	if err := s.client.getJSON(fmt.Sprintf("projects/%s/repos/%s/pull-requests/%d", project, slug, id), &raw); err != nil {
		return err
	}
	endpoint := fmt.Sprintf("projects/%s/repos/%s/pull-requests/%d/merge?version=%d", project, slug, id, raw.Version)
	if err := s.client.postJSON(endpoint, nil, nil); err != nil {
		return fmt.Errorf("merge PR %d: %w", pr.ID, err)
	}
	return nil
}

func (s *serverService) DeclinePR(project, slug string, id int) error {
	var raw struct{ Version int `json:"version"` }
	if err := s.client.getJSON(fmt.Sprintf("projects/%s/repos/%s/pull-requests/%d", project, slug, id), &raw); err != nil {
		return err
	}
	endpoint := fmt.Sprintf("projects/%s/repos/%s/pull-requests/%d/decline?version=%d", project, slug, id, raw.Version)
	return s.client.postJSON(endpoint, nil, nil)
}

func (s *serverService) PRDiff(project, slug string, id int) (string, error) {
	endpoint := fmt.Sprintf("projects/%s/repos/%s/pull-requests/%d.diff", project, slug, id)
	req, err := s.client.NewRequest("GET", endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "text/plain")
	resp, err := s.client.Do(req)
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

// ListBuildsForRef hits the build-status API. Server gives us builds keyed
// by commit SHA, so we first resolve the latest commit on the ref.
func (s *serverService) ListBuildsForRef(project, slug, ref string, limit int) ([]Build, error) {
	if ref == "" {
		repo, err := s.GetRepo(project, slug)
		if err != nil {
			return nil, err
		}
		ref = repo.DefaultRef
	}

	// Resolve ref → commit.
	var commits srvPaged[struct{ ID string `json:"id"` }]
	endpoint := fmt.Sprintf("projects/%s/repos/%s/commits%s", project, slug, queryString(map[string]string{"until": ref, "limit": "1"}))
	if err := s.client.getJSON(endpoint, &commits); err != nil {
		return nil, err
	}
	if len(commits.Values) == 0 {
		return nil, fmt.Errorf("no commits found for ref %q", ref)
	}
	commit := commits.Values[0].ID

	// Build statuses live under a different base URL. Build the absolute URL.
	base := strings.Replace(strings.TrimRight(s.client.cfg.APIBase, "/"), "/rest/api/1.0", "", 1)
	url := fmt.Sprintf("%s/rest/build-status/1.0/commits/%s%s", base, commit, queryString(map[string]string{"limit": itoa(limit)}))
	var page srvPaged[srvBuild]
	if err := s.client.getJSON(url, &page); err != nil {
		return nil, err
	}
	out := make([]Build, 0, len(page.Values))
	for _, b := range page.Values {
		out = append(out, Build{
			ID:        b.Key,
			Name:      b.Name,
			State:     b.State,
			URL:       b.URL,
			Ref:       ref,
			Commit:    commit,
			CreatedAt: time.UnixMilli(b.DateAdded),
		})
	}
	return out, nil
}
