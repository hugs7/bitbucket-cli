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
func (s *serverService) Me() string   { return s.client.cfg.Username }

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

type srvParticipant struct {
	User     srvUser `json:"user"`
	Approved bool    `json:"approved"`
	Status   string  `json:"status"`
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
	Reviewers   []srvParticipant `json:"reviewers"`
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
	for _, r := range p.Reviewers {
		out.Reviewers = append(out.Reviewers, Reviewer{
			Username:    r.User.Name,
			DisplayName: r.User.DisplayName,
			Status:      r.Status,
			Approved:    r.Approved,
		})
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
	// Bitbucket Server accepts state=ALL to mean "any state".
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

// --- mutating actions ---

func (s *serverService) prVersion(project, slug string, id int) (int, error) {
	var raw struct{ Version int `json:"version"` }
	if err := s.client.getJSON(fmt.Sprintf("projects/%s/repos/%s/pull-requests/%d", project, slug, id), &raw); err != nil {
		return 0, err
	}
	return raw.Version, nil
}

func (s *serverService) UpdatePRDescription(project, slug string, id int, description string) error {
	v, err := s.prVersion(project, slug, id)
	if err != nil {
		return err
	}
	body := map[string]any{"version": v, "description": description}
	return s.client.putJSON(fmt.Sprintf("projects/%s/repos/%s/pull-requests/%d", project, slug, id), body, nil)
}

func (s *serverService) participantStatus(project, slug string, id int, status string, approved bool) error {
	user := s.client.cfg.Username
	if user == "" {
		return fmt.Errorf("no username configured for %s", s.host)
	}
	body := map[string]any{
		"user":     map[string]string{"name": user},
		"approved": approved,
		"status":   status,
	}
	return s.client.putJSON(fmt.Sprintf("projects/%s/repos/%s/pull-requests/%d/participants/%s", project, slug, id, user), body, nil)
}

func (s *serverService) ApprovePR(project, slug string, id int) error {
	return s.participantStatus(project, slug, id, "APPROVED", true)
}
func (s *serverService) UnapprovePR(project, slug string, id int) error {
	return s.participantStatus(project, slug, id, "UNAPPROVED", false)
}
func (s *serverService) NeedsWorkPR(project, slug string, id int) error {
	return s.participantStatus(project, slug, id, "NEEDS_WORK", false)
}

// --- comments ---

type srvComment struct {
	ID          int     `json:"id"`
	Text        string  `json:"text"`
	Author      srvUser `json:"author"`
	CreatedDate int64   `json:"createdDate"`
	UpdatedDate int64   `json:"updatedDate"`
}
type srvCommentAnchor struct {
	Path     string `json:"path"`
	Line     int    `json:"line"`
	LineType string `json:"lineType"` // ADDED | REMOVED | CONTEXT
	FileType string `json:"fileType"` // TO (new) | FROM (old)
	DiffType string `json:"diffType"`
}

type srvActivity struct {
	ID            int               `json:"id"`
	Action        string            `json:"action"`
	Comment       srvComment        `json:"comment"`
	CommentAnchor *srvCommentAnchor `json:"commentAnchor"`
}

func (s *serverService) ListComments(project, slug string, id int) ([]Comment, error) {
	var page srvPaged[srvActivity]
	endpoint := fmt.Sprintf("projects/%s/repos/%s/pull-requests/%d/activities%s",
		project, slug, id,
		queryString(map[string]string{"limit": "100"}),
	)
	if err := s.client.getJSON(endpoint, &page); err != nil {
		return nil, err
	}
	out := make([]Comment, 0)
	for _, a := range page.Values {
		if a.Action != "COMMENTED" || a.Comment.ID == 0 {
			continue
		}
		c := Comment{
			ID:        a.Comment.ID,
			Author:    a.Comment.Author.DisplayName,
			Text:      a.Comment.Text,
			CreatedAt: time.UnixMilli(a.Comment.CreatedDate),
			UpdatedAt: time.UnixMilli(a.Comment.UpdatedDate),
		}
		if a.CommentAnchor != nil && a.CommentAnchor.Path != "" {
			// LineType ADDED → new side; REMOVED → old side; CONTEXT
			// can sit on either, default to new. fileType FROM is a
			// stronger signal that the comment is on the old side.
			side := "new"
			if a.CommentAnchor.LineType == "REMOVED" || a.CommentAnchor.FileType == "FROM" {
				side = "old"
			}
			c.Inline = &CommentInline{
				Path: a.CommentAnchor.Path,
				Line: a.CommentAnchor.Line,
				Side: side,
			}
		}
		out = append(out, c)
	}
	return out, nil
}

func (s *serverService) AddComment(project, slug string, id int, text string) (*Comment, error) {
	body := map[string]any{"text": text}
	var c srvComment
	if err := s.client.postJSON(fmt.Sprintf("projects/%s/repos/%s/pull-requests/%d/comments", project, slug, id), body, &c); err != nil {
		return nil, err
	}
	return &Comment{
		ID:        c.ID,
		Author:    c.Author.DisplayName,
		Text:      c.Text,
		CreatedAt: time.UnixMilli(c.CreatedDate),
		UpdatedAt: time.UnixMilli(c.UpdatedDate),
	}, nil
}

// AddInlineComment posts a comment anchored to a file + line in the diff.
// On Server: lineType / fileType encode whether we're commenting on the
// added (RHS) or removed (LHS) side of the diff. Line==0 means a
// file-level comment (path-only anchor).
func (s *serverService) AddInlineComment(project, slug string, prID int, in InlineCommentInput) (*Comment, error) {
	side := strings.ToLower(in.Side)
	if side == "" {
		side = "new"
	}
	lineType, fileType := "ADDED", "TO"
	if side == "old" {
		lineType, fileType = "REMOVED", "FROM"
	}
	anchor := map[string]any{
		"path":     in.Path,
		"fileType": fileType,
		"diffType": "EFFECTIVE",
	}
	if in.Line > 0 {
		anchor["line"] = in.Line
		anchor["lineType"] = lineType
	}
	body := map[string]any{
		"text":   in.Text,
		"anchor": anchor,
	}
	var c srvComment
	if err := s.client.postJSON(fmt.Sprintf("projects/%s/repos/%s/pull-requests/%d/comments", project, slug, prID), body, &c); err != nil {
		return nil, err
	}
	return &Comment{
		ID: c.ID, Author: c.Author.DisplayName, Text: c.Text,
		CreatedAt: time.UnixMilli(c.CreatedDate), UpdatedAt: time.UnixMilli(c.UpdatedDate),
	}, nil
}

// AddReaction adds an emoji reaction to a comment. Bitbucket Server
// (DC 7.x+) accepts a PUT to /comments/{id}/reactions/{emoji} where
// emoji is a short name like "thumbsup".
func (s *serverService) AddReaction(project, slug string, prID, commentID int, emoji string) error {
	if emoji == "" {
		emoji = "thumbsup"
	}
	endpoint := fmt.Sprintf("projects/%s/repos/%s/pull-requests/%d/comments/%d/reactions/%s",
		project, slug, prID, commentID, emoji)
	req, err := s.client.NewRequest("PUT", endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	return decode(resp, nil)
}

// PipelineLogs is not supported on Server (no native pipelines product).
func (s *serverService) PipelineLogs(project, slug, idOrUUID string) (string, error) {
	return "", fmt.Errorf("downloading build logs is not supported on Bitbucket Server (use your CI system)")
}

// commentVersion fetches the current version of a comment, needed for
// edit/delete on Bitbucket Server.
func (s *serverService) commentVersion(project, slug string, prID, commentID int) (int, error) {
	var raw struct{ Version int `json:"version"` }
	if err := s.client.getJSON(fmt.Sprintf("projects/%s/repos/%s/pull-requests/%d/comments/%d", project, slug, prID, commentID), &raw); err != nil {
		return 0, err
	}
	return raw.Version, nil
}

func (s *serverService) EditComment(project, slug string, prID, commentID int, text string) (*Comment, error) {
	v, err := s.commentVersion(project, slug, prID, commentID)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"version": v, "text": text}
	var c srvComment
	if err := s.client.putJSON(fmt.Sprintf("projects/%s/repos/%s/pull-requests/%d/comments/%d", project, slug, prID, commentID), body, &c); err != nil {
		return nil, err
	}
	return &Comment{
		ID: c.ID, Author: c.Author.DisplayName, Text: c.Text,
		CreatedAt: time.UnixMilli(c.CreatedDate), UpdatedAt: time.UnixMilli(c.UpdatedDate),
	}, nil
}

func (s *serverService) DeleteComment(project, slug string, prID, commentID int) error {
	v, err := s.commentVersion(project, slug, prID, commentID)
	if err != nil {
		return err
	}
	return s.client.deleteJSON(fmt.Sprintf("projects/%s/repos/%s/pull-requests/%d/comments/%d?version=%d", project, slug, prID, commentID, v))
}

func (s *serverService) ReplyComment(project, slug string, prID, parentID int, text string) (*Comment, error) {
	body := map[string]any{
		"text":   text,
		"parent": map[string]int{"id": parentID},
	}
	var c srvComment
	if err := s.client.postJSON(fmt.Sprintf("projects/%s/repos/%s/pull-requests/%d/comments", project, slug, prID), body, &c); err != nil {
		return nil, err
	}
	return &Comment{
		ID: c.ID, Author: c.Author.DisplayName, Text: c.Text,
		CreatedAt: time.UnixMilli(c.CreatedDate), UpdatedAt: time.UnixMilli(c.UpdatedDate),
	}, nil
}

// AddReviewers / RemoveReviewers update the PR's reviewers list. Server
// requires the full set in a PUT against the PR (with version).
func (s *serverService) updateReviewers(project, slug string, prID int, mutate func([]string) []string) error {
	pr, err := s.GetPR(project, slug, prID)
	if err != nil {
		return err
	}
	current := make([]string, 0, len(pr.Reviewers))
	for _, r := range pr.Reviewers {
		if r.Username != "" {
			current = append(current, r.Username)
		}
	}
	updated := mutate(current)

	v, err := s.prVersion(project, slug, prID)
	if err != nil {
		return err
	}
	reviewers := make([]map[string]any, 0, len(updated))
	for _, u := range updated {
		reviewers = append(reviewers, map[string]any{"user": map[string]string{"name": u}})
	}
	body := map[string]any{"version": v, "reviewers": reviewers}
	return s.client.putJSON(fmt.Sprintf("projects/%s/repos/%s/pull-requests/%d", project, slug, prID), body, nil)
}

func (s *serverService) AddReviewers(project, slug string, prID int, usernames []string) error {
	return s.updateReviewers(project, slug, prID, func(cur []string) []string {
		seen := map[string]bool{}
		for _, u := range cur {
			seen[u] = true
		}
		for _, u := range usernames {
			if u != "" && !seen[u] {
				cur = append(cur, u)
				seen[u] = true
			}
		}
		return cur
	})
}

func (s *serverService) RemoveReviewers(project, slug string, prID int, usernames []string) error {
	return s.updateReviewers(project, slug, prID, func(cur []string) []string {
		drop := map[string]bool{}
		for _, u := range usernames {
			drop[u] = true
		}
		out := cur[:0]
		for _, u := range cur {
			if !drop[u] {
				out = append(out, u)
			}
		}
		return out
	})
}

// --- Webhooks ---

type srvWebhook struct {
	ID     int      `json:"id"`
	Name   string   `json:"name"`
	URL    string   `json:"url"`
	Events []string `json:"events"`
	Active bool     `json:"active"`
}

func (w srvWebhook) toWebhook() Webhook {
	return Webhook{
		ID:          itoa(w.ID),
		URL:         w.URL,
		Description: w.Name,
		Events:      w.Events,
		Active:      w.Active,
	}
}

func (s *serverService) ListWebhooks(project, slug string) ([]Webhook, error) {
	var page srvPaged[srvWebhook]
	if err := s.client.getJSON(fmt.Sprintf("projects/%s/repos/%s/webhooks", project, slug), &page); err != nil {
		return nil, err
	}
	out := make([]Webhook, 0, len(page.Values))
	for _, w := range page.Values {
		out = append(out, w.toWebhook())
	}
	return out, nil
}

func (s *serverService) AddWebhook(project, slug string, in WebhookInput) (*Webhook, error) {
	name := in.Description
	if name == "" {
		name = "bb-webhook"
	}
	body := map[string]any{
		"name":   name,
		"url":    in.URL,
		"events": in.Events,
		"active": in.Active,
	}
	var w srvWebhook
	if err := s.client.postJSON(fmt.Sprintf("projects/%s/repos/%s/webhooks", project, slug), body, &w); err != nil {
		return nil, err
	}
	out := w.toWebhook()
	return &out, nil
}

func (s *serverService) DeleteWebhook(project, slug, id string) error {
	return s.client.deleteJSON(fmt.Sprintf("projects/%s/repos/%s/webhooks/%s", project, slug, id))
}

// CreateRepo creates a new repository in the given project.
func (s *serverService) CreateRepo(in CreateRepoInput) (*Repo, error) {
	scm := in.SCM
	if scm == "" {
		scm = "git"
	}
	body := map[string]any{"name": in.Name, "scmId": scm}
	if in.Description != "" {
		body["description"] = in.Description
	}
	var r srvRepo
	if err := s.client.postJSON(fmt.Sprintf("projects/%s/repos", in.Project), body, &r); err != nil {
		return nil, err
	}
	out := r.toRepo()
	return &out, nil
}

// srvSearchResponse models the relevant slice of a /rest/search/latest/search
// response. The endpoint is provided by Bitbucket's bundled "search"
// plugin (the same one that powers the dashboard's quick-search UI)
// and is dramatically faster than paginating /rest/api/1.0/repos.
//
// Repositories are returned as flat srvRepo objects (NOT wrapped in
// `{"repository": {...}}` like the audit-style search APIs).
type srvSearchResponse struct {
	Repositories struct {
		Values     []srvRepo `json:"values"`
		IsLastPage bool      `json:"isLastPage"`
		Count      int       `json:"count"`
	} `json:"repositories"`
}

// SearchRepos searches across projects on Bitbucket Server.
//
// Strategy:
//  1. POST to /rest/search/latest/search — the same endpoint the
//     dashboard uses. It does fuzzy / substring matching server-side
//     and returns in well under a second even on large instances.
//  2. If the search plugin isn't available (older Server installs),
//     fall back to the slow paginated /repos scan with a client-side
//     substring filter so behaviour is still correct.
//
// An empty query falls straight to the legacy listing path because
// the search endpoint requires a non-empty query.
func (s *serverService) SearchRepos(query string, limit int) ([]Repo, error) {
	if limit <= 0 {
		limit = 50
	}
	q := strings.TrimSpace(query)

	if q != "" {
		if repos, ok := s.searchReposViaPlugin(q, limit); ok {
			return repos, nil
		}
	}
	return s.searchReposViaScan(q, limit)
}

// searchReposViaPlugin calls the dashboard search endpoint. Returns
// (repos, true) on success or (nil, false) if the endpoint is
// unavailable (404, plugin disabled, etc) so the caller can fall back.
func (s *serverService) searchReposViaPlugin(query string, limit int) ([]Repo, bool) {
	endpoint := s.client.HostRoot() + "/rest/search/latest/search"
	body := map[string]any{
		"query":    query,
		"entities": map[string]any{"repositories": map[string]any{}},
		"limits":   map[string]any{"primary": limit},
	}
	var out srvSearchResponse
	if err := s.client.postJSON(endpoint, body, &out); err != nil {
		return nil, false
	}
	repos := make([]Repo, 0, len(out.Repositories.Values))
	for _, r := range out.Repositories.Values {
		repo := r.toRepo()
		// The search-plugin response omits Links, so synthesise the
		// browse URL from the host root + project/slug so the `o`
		// (open in browser) action still works on search results.
		if repo.WebURL == "" && repo.Project != "" && repo.Slug != "" {
			repo.WebURL = fmt.Sprintf("%s/projects/%s/repos/%s/browse",
				s.client.HostRoot(), repo.Project, repo.Slug)
		}
		repos = append(repos, repo)
		if len(repos) >= limit {
			break
		}
	}
	return repos, true
}

// searchReposViaScan is the legacy fallback: prefix-match via /repos?name=
// then a paginated substring sweep. Bounded by maxPages × pageSize.
func (s *serverService) searchReposViaScan(query string, limit int) ([]Repo, error) {
	q := strings.ToLower(query)
	out := []Repo{}
	seen := map[string]bool{}
	add := func(r Repo) bool {
		k := r.Project + "/" + r.Slug
		if seen[k] {
			return false
		}
		seen[k] = true
		out = append(out, r)
		return len(out) >= limit
	}

	if q != "" {
		var page srvPaged[srvRepo]
		params := map[string]string{"limit": itoa(limit), "name": query}
		if err := s.client.getJSON("repos"+queryString(params), &page); err == nil {
			for _, r := range page.Values {
				if add(r.toRepo()) {
					return out, nil
				}
			}
		}
	}

	const pageSize = 500
	const maxPages = 100
	start := 0
	for p := 0; p < maxPages; p++ {
		var page srvPaged[srvRepo]
		params := map[string]string{"limit": itoa(pageSize), "start": itoa(start)}
		if err := s.client.getJSON("repos"+queryString(params), &page); err != nil {
			return out, nil
		}
		for _, r := range page.Values {
			repo := r.toRepo()
			if q == "" ||
				strings.Contains(strings.ToLower(repo.Slug), q) ||
				strings.Contains(strings.ToLower(repo.Name), q) ||
				strings.Contains(strings.ToLower(repo.Project), q) {
				if add(repo) {
					return out, nil
				}
			}
		}
		if page.IsLastPage || len(page.Values) == 0 {
			break
		}
		start += len(page.Values)
	}
	return out, nil
}

// ListMyReviewPRs uses Server's /inbox/pull-requests endpoint which
// returns PRs the authenticated user is a reviewer on.
func (s *serverService) ListMyReviewPRs(limit int) ([]ReviewPR, error) {
	if limit <= 0 {
		limit = 50
	}
	endpoint := "inbox/pull-requests" + queryString(map[string]string{
		"role":  "REVIEWER",
		"state": "OPEN",
		"limit": itoa(limit),
	})
	return s.fetchDashboardPRs(endpoint)
}

// ListMyAuthoredPRs returns open PRs authored by the current user via
// /dashboard/pull-requests?role=AUTHOR — the same endpoint that
// powers the dashboard's "Your pull requests" section.
func (s *serverService) ListMyAuthoredPRs(limit int) ([]ReviewPR, error) {
	if limit <= 0 {
		limit = 25
	}
	endpoint := "dashboard/pull-requests" + queryString(map[string]string{
		"role":  "AUTHOR",
		"state": "OPEN",
		"limit": itoa(limit),
		"order": "NEWEST",
	})
	return s.fetchDashboardPRs(endpoint)
}

// ListRecentlyClosedPRs returns the user's most recently merged PRs
// (across all roles) so the home dashboard can show a "what
// shipped" feed.
func (s *serverService) ListRecentlyClosedPRs(limit int) ([]ReviewPR, error) {
	if limit <= 0 {
		limit = 25
	}
	endpoint := "dashboard/pull-requests" + queryString(map[string]string{
		"state": "MERGED",
		"limit": itoa(limit),
		"order": "NEWEST",
	})
	return s.fetchDashboardPRs(endpoint)
}

// fetchDashboardPRs is the shared decoder for any dashboard / inbox
// endpoint that returns PRs with their toRef.repository inlined.
func (s *serverService) fetchDashboardPRs(endpoint string) ([]ReviewPR, error) {
	type srvDashPR struct {
		srvPR
		ToRef struct {
			srvRef
			Repository srvRepo `json:"repository"`
		} `json:"toRef"`
	}
	var page srvPaged[srvDashPR]
	if err := s.client.getJSON(endpoint, &page); err != nil {
		return nil, err
	}
	out := make([]ReviewPR, 0, len(page.Values))
	for _, p := range page.Values {
		out = append(out, ReviewPR{
			PR:      p.srvPR.toPR(),
			Project: p.ToRef.Repository.Project.Key,
			Slug:    p.ToRef.Repository.Slug,
		})
	}
	return out, nil
}

// ListRecentlyViewedRepos returns repos the user has opened recently
// in the Bitbucket UI. Uses /profile/recent/repos which is dramatically
// faster than scanning the full repo list.
func (s *serverService) ListRecentlyViewedRepos(limit int) ([]Repo, error) {
	if limit <= 0 {
		limit = 10
	}
	var page srvPaged[srvRepo]
	endpoint := "profile/recent/repos" + queryString(map[string]string{"limit": itoa(limit)})
	if err := s.client.getJSON(endpoint, &page); err != nil {
		return nil, err
	}
	out := make([]Repo, 0, len(page.Values))
	for _, r := range page.Values {
		out = append(out, r.toRepo())
	}
	return out, nil
}

// GetReadme fetches a README from the repo's default branch. Tries
// multiple ref + filename combinations so it works whether the
// caller knows the default branch and whether the README has the
// canonical capitalisation.
func (s *serverService) GetReadme(project, slug string) (string, error) {
	// Resolve default branch — first try the repo metadata, then
	// /branches/default, then common fallback names.
	refs := []string{}
	if repo, err := s.GetRepo(project, slug); err == nil && repo.DefaultRef != "" {
		refs = append(refs, repo.DefaultRef)
	}
	var def struct {
		ID        string `json:"id"`
		DisplayID string `json:"displayId"`
	}
	if err := s.client.getJSON(fmt.Sprintf("projects/%s/repos/%s/branches/default", project, slug), &def); err == nil {
		if def.DisplayID != "" {
			refs = append(refs, def.DisplayID)
		}
		if def.ID != "" {
			refs = append(refs, def.ID)
		}
	}
	refs = append(refs, "master", "main", "develop", "trunk")

	names := []string{"README.md", "README.MD", "Readme.md", "readme.md", "README", "README.txt"}
	for _, ref := range refs {
		for _, name := range names {
			endpoint := fmt.Sprintf("projects/%s/repos/%s/raw/%s%s",
				project, slug, name,
				queryString(map[string]string{"at": ref}),
			)
			req, err := s.client.NewRequest("GET", endpoint, nil)
			if err != nil {
				continue
			}
			req.Header.Set("Accept", "text/plain")
			resp, err := s.client.Do(req)
			if err != nil {
				continue
			}
			if resp.StatusCode >= 400 {
				resp.Body.Close()
				continue
			}
			b, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				continue
			}
			return string(b), nil
		}
	}
	return "", nil
}

// Bitbucket Server does not have a built-in pipelines system; CI is
// reported via build-status. Trigger/cancel are CI-system specific.
func (s *serverService) TriggerPipeline(project, slug, ref string) (*Build, error) {
	return nil, fmt.Errorf("triggering builds is not supported on Bitbucket Server (use your CI system, e.g. Bamboo / Jenkins)")
}
func (s *serverService) CancelPipeline(project, slug, idOrUUID string) error {
	return fmt.Errorf("cancelling builds is not supported on Bitbucket Server (use your CI system)")
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
