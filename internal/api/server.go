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
// added (RHS) or removed (LHS) side of the diff.
func (s *serverService) AddInlineComment(project, slug string, prID int, in InlineCommentInput) (*Comment, error) {
	side := strings.ToLower(in.Side)
	if side == "" {
		side = "new"
	}
	lineType, fileType := "ADDED", "TO"
	if side == "old" {
		lineType, fileType = "REMOVED", "FROM"
	}
	body := map[string]any{
		"text": in.Text,
		"anchor": map[string]any{
			"line":     in.Line,
			"lineType": lineType,
			"fileType": fileType,
			"path":     in.Path,
			"diffType": "EFFECTIVE",
		},
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

// SearchRepos searches across projects on Bitbucket Server.
func (s *serverService) SearchRepos(query string, limit int) ([]Repo, error) {
	if limit <= 0 {
		limit = 50
	}
	params := map[string]string{"limit": itoa(limit)}
	if query != "" {
		params["name"] = query
	}
	var page srvPaged[srvRepo]
	if err := s.client.getJSON("repos"+queryString(params), &page); err != nil {
		return nil, err
	}
	out := make([]Repo, 0, len(page.Values))
	for _, r := range page.Values {
		out = append(out, r.toRepo())
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
	// /inbox is hosted at /rest/api/latest, but our base is /rest/api/1.0;
	// /1.0 also serves /inbox/pull-requests.
	type srvInboxPR struct {
		srvPR
		ToRef struct {
			srvRef
			Repository srvRepo `json:"repository"`
		} `json:"toRef"`
	}
	var page srvPaged[srvInboxPR]
	if err := s.client.getJSON(endpoint, &page); err != nil {
		return nil, err
	}
	out := make([]ReviewPR, 0, len(page.Values))
	for _, p := range page.Values {
		pr := p.srvPR.toPR()
		out = append(out, ReviewPR{
			PR:      pr,
			Project: p.ToRef.Repository.Project.Key,
			Slug:    p.ToRef.Repository.Slug,
		})
	}
	return out, nil
}

// GetReadme fetches a README from the repo's default branch.
func (s *serverService) GetReadme(project, slug string) (string, error) {
	repo, err := s.GetRepo(project, slug)
	if err != nil {
		return "", err
	}
	ref := repo.DefaultRef
	if ref == "" {
		ref = "main"
	}
	for _, name := range []string{"README.md", "README.MD", "Readme.md", "README", "README.txt"} {
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
