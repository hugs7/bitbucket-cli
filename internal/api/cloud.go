package api

import (
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
)

// cloudService implements Service for Bitbucket Cloud (REST 2.0).
type cloudService struct {
	client *Client
	host   string
}

func (c *cloudService) Host() string { return c.host }
func (c *cloudService) Me() string   { return c.client.cfg.Username }

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
	UUID        string `json:"uuid"`
	DisplayName string `json:"display_name"`
	Nickname    string `json:"nickname"`
}

type clBranch struct {
	Branch struct{ Name string `json:"name"` } `json:"branch"`
}

type clParticipant struct {
	User     clUser `json:"user"`
	Role     string `json:"role"`
	Approved bool   `json:"approved"`
	State    string `json:"state"` // "approved" / "changes_requested" / null
}

type clPR struct {
	ID           int             `json:"id"`
	Title        string          `json:"title"`
	Description  string          `json:"summary"`
	State        string          `json:"state"`
	CreatedOn    string          `json:"created_on"`
	UpdatedOn    string          `json:"updated_on"`
	Author       clUser          `json:"author"`
	Source       clBranch        `json:"source"`
	Destination  clBranch        `json:"destination"`
	Reviewers    []clUser        `json:"reviewers"`
	Participants []clParticipant `json:"participants"`
	Links        clLinks         `json:"links"`
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
	out := PullRequest{
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
	// Map participants for approval state when present, else fall back
	// to the bare reviewers list. Cloud's "Username" identity is the
	// UUID — that's what subsequent updates need to reference.
	approval := map[string]clParticipant{}
	for _, pa := range p.Participants {
		if pa.Role == "REVIEWER" {
			approval[pa.User.UUID] = pa
		}
	}
	for _, r := range p.Reviewers {
		rev := Reviewer{
			Username:    r.UUID,
			DisplayName: r.DisplayName,
		}
		if pa, ok := approval[r.UUID]; ok {
			rev.Approved = pa.Approved
			rev.Status = strings.ToUpper(pa.State)
		}
		out.Reviewers = append(out.Reviewers, rev)
	}
	return out
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

func (c *cloudService) MergePR(workspace, slug string, id int, strategyID string) error {
	body := map[string]any{}
	if strategyID != "" {
		body["merge_strategy"] = strategyID
	}
	return c.client.postJSON(fmt.Sprintf("repositories/%s/%s/pullrequests/%d/merge", workspace, slug, id), body, nil)
}

// MergeStrategies returns the three documented Bitbucket Cloud merge
// strategies. Cloud doesn't expose a per-repo allowed-list endpoint
// (branch restrictions of kind "allow_merge_strategies" can hide
// some at the branch level but aren't queryable cheaply); we list
// all three and let the merge endpoint surface a clear error if the
// repo restricts the chosen one.
func (c *cloudService) MergeStrategies(workspace, slug string) ([]MergeStrategy, error) {
	return []MergeStrategy{
		{ID: "merge_commit", Name: "Merge commit", Default: true},
		{ID: "squash", Name: "Squash"},
		{ID: "fast_forward", Name: "Fast-forward"},
	}, nil
}

func (c *cloudService) DeclinePR(workspace, slug string, id int) error {
	return c.client.postJSON(fmt.Sprintf("repositories/%s/%s/pullrequests/%d/decline", workspace, slug, id), nil, nil)
}

// DeletePR is not supported by Bitbucket Cloud — the REST 2.0 API
// has no DELETE on /pullrequests/{id}. Decline + close is as far as
// the platform goes. Surface a clear error so the TUI can show a
// useful toast instead of an HTTP 405.
func (c *cloudService) DeletePR(workspace, slug string, id int) error {
	return fmt.Errorf("deleting PRs is not supported on Bitbucket Cloud (decline closes the PR)")
}

// DeleteBranch removes a branch from the remote repo via Cloud's
// refs/branches endpoint. URL-encoded so feature/foo branches work.
func (c *cloudService) DeleteBranch(workspace, slug, branch string) error {
	return c.client.deleteJSON(fmt.Sprintf("repositories/%s/%s/refs/branches/%s",
		workspace, slug, url.PathEscape(branch)))
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

func (c *cloudService) UpdatePRTarget(workspace, slug string, id int, targetRef string) error {
	body := map[string]any{
		"destination": map[string]any{
			"branch": map[string]string{"name": targetRef},
		},
	}
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
		Inline  *struct {
			Path string `json:"path"`
			From *int   `json:"from"` // line on old (LHS) side
			To   *int   `json:"to"`   // line on new (RHS) side
		} `json:"inline"`
	}
	var page clPaged[cc]
	if err := c.client.getJSON(fmt.Sprintf("repositories/%s/%s/pullrequests/%d/comments?pagelen=100", workspace, slug, id), &page); err != nil {
		return nil, err
	}
	out := make([]Comment, 0, len(page.Values))
	for _, x := range page.Values {
		comment := Comment{
			ID: x.ID, Author: x.User.DisplayName, Text: x.Content.Raw,
			CreatedAt: parseTime(x.Created), UpdatedAt: parseTime(x.Updated),
		}
		if x.Inline != nil && x.Inline.Path != "" {
			// Cloud sets either `to` (new side), `from` (old side),
			// or neither (file-level comment, Line stays 0).
			ic := &CommentInline{Path: x.Inline.Path, Side: "new"}
			switch {
			case x.Inline.To != nil:
				ic.Line = *x.Inline.To
				ic.Side = "new"
			case x.Inline.From != nil:
				ic.Line = *x.Inline.From
				ic.Side = "old"
			}
			comment.Inline = ic
		}
		out = append(out, comment)
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

type cloudCC struct {
	ID      int    `json:"id"`
	Content struct{ Raw string `json:"raw"` } `json:"content"`
	User    clUser `json:"user"`
	Created string `json:"created_on"`
	Updated string `json:"updated_on"`
}

func (cc cloudCC) toComment() Comment {
	return Comment{
		ID: cc.ID, Author: cc.User.DisplayName, Text: cc.Content.Raw,
		CreatedAt: parseTime(cc.Created), UpdatedAt: parseTime(cc.Updated),
	}
}

func (c *cloudService) EditComment(workspace, slug string, prID, commentID int, text string) (*Comment, error) {
	body := map[string]any{"content": map[string]string{"raw": text}}
	var resp cloudCC
	if err := c.client.putJSON(fmt.Sprintf("repositories/%s/%s/pullrequests/%d/comments/%d", workspace, slug, prID, commentID), body, &resp); err != nil {
		return nil, err
	}
	out := resp.toComment()
	return &out, nil
}

func (c *cloudService) DeleteComment(workspace, slug string, prID, commentID int) error {
	return c.client.deleteJSON(fmt.Sprintf("repositories/%s/%s/pullrequests/%d/comments/%d", workspace, slug, prID, commentID))
}

func (c *cloudService) ReplyComment(workspace, slug string, prID, parentID int, text string) (*Comment, error) {
	body := map[string]any{
		"content": map[string]string{"raw": text},
		"parent":  map[string]int{"id": parentID},
	}
	var resp cloudCC
	if err := c.client.postJSON(fmt.Sprintf("repositories/%s/%s/pullrequests/%d/comments", workspace, slug, prID), body, &resp); err != nil {
		return nil, err
	}
	out := resp.toComment()
	return &out, nil
}

// resolveUserUUID maps a Cloud username/nickname to its `{xxx}` UUID,
// which is what reviewer mutations require. Pass-through if the input
// already looks like a UUID.
func (c *cloudService) resolveUserUUID(username string) (string, error) {
	if strings.HasPrefix(username, "{") && strings.HasSuffix(username, "}") {
		return username, nil
	}
	var u clUser
	if err := c.client.getJSON(fmt.Sprintf("users/%s", username), &u); err != nil {
		return "", fmt.Errorf("resolve user %q: %w", username, err)
	}
	if u.UUID == "" {
		return "", fmt.Errorf("user %q has no uuid in response", username)
	}
	return u.UUID, nil
}

// updateReviewers PUTs the full reviewer set against the PR. Cloud is
// wholesale (like Server) but does not require a version field.
func (c *cloudService) updateReviewers(workspace, slug string, prID int, mutate func([]string) []string) error {
	pr, err := c.GetPR(workspace, slug, prID)
	if err != nil {
		return err
	}
	current := make([]string, 0, len(pr.Reviewers))
	for _, r := range pr.Reviewers {
		if r.Username != "" {
			current = append(current, r.Username) // already UUIDs
		}
	}
	updated := mutate(current)
	reviewers := make([]map[string]string, 0, len(updated))
	for _, uuid := range updated {
		reviewers = append(reviewers, map[string]string{"uuid": uuid})
	}
	body := map[string]any{"reviewers": reviewers}
	return c.client.putJSON(fmt.Sprintf("repositories/%s/%s/pullrequests/%d", workspace, slug, prID), body, nil)
}

func (c *cloudService) AddReviewers(workspace, slug string, prID int, usernames []string) error {
	uuids := make([]string, 0, len(usernames))
	for _, u := range usernames {
		if u == "" {
			continue
		}
		uuid, err := c.resolveUserUUID(u)
		if err != nil {
			return err
		}
		uuids = append(uuids, uuid)
	}
	return c.updateReviewers(workspace, slug, prID, func(cur []string) []string {
		seen := map[string]bool{}
		for _, u := range cur {
			seen[u] = true
		}
		for _, u := range uuids {
			if !seen[u] {
				cur = append(cur, u)
				seen[u] = true
			}
		}
		return cur
	})
}

func (c *cloudService) RemoveReviewers(workspace, slug string, prID int, usernames []string) error {
	drop := map[string]bool{}
	for _, u := range usernames {
		if u == "" {
			continue
		}
		uuid, err := c.resolveUserUUID(u)
		if err != nil {
			return err
		}
		drop[uuid] = true
	}
	return c.updateReviewers(workspace, slug, prID, func(cur []string) []string {
		out := cur[:0]
		for _, u := range cur {
			if !drop[u] {
				out = append(out, u)
			}
		}
		return out
	})
}

// AddInlineComment posts a line-anchored comment.
// Cloud uses inline.to (line on new side) or inline.from (line on old
// side). When in.Line is 0 the comment is file-level (path-only).
func (c *cloudService) AddInlineComment(workspace, slug string, prID int, in InlineCommentInput) (*Comment, error) {
	side := strings.ToLower(in.Side)
	if side == "" {
		side = "new"
	}
	inline := map[string]any{"path": in.Path}
	if in.Line > 0 {
		if side == "old" {
			inline["from"] = in.Line
		} else {
			inline["to"] = in.Line
		}
	}
	body := map[string]any{
		"content": map[string]string{"raw": in.Text},
		"inline":  inline,
	}
	var resp cloudCC
	if err := c.client.postJSON(fmt.Sprintf("repositories/%s/%s/pullrequests/%d/comments", workspace, slug, prID), body, &resp); err != nil {
		return nil, err
	}
	out := resp.toComment()
	return &out, nil
}

// AddReaction is not supported on Bitbucket Cloud (no public REST
// endpoint for comment reactions exists).
func (c *cloudService) AddReaction(workspace, slug string, prID, commentID int, emoji string) error {
	return fmt.Errorf("comment reactions are not supported on Bitbucket Cloud")
}

// PipelineLogs concatenates the logs of all steps in a pipeline run.
func (c *cloudService) PipelineLogs(workspace, slug, idOrUUID string) (string, error) {
	id := strings.TrimPrefix(idOrUUID, "#")

	type step struct {
		UUID string `json:"uuid"`
		Name string `json:"name"`
	}
	var steps clPaged[step]
	if err := c.client.getJSON(fmt.Sprintf("repositories/%s/%s/pipelines/%s/steps/", workspace, slug, id), &steps); err != nil {
		return "", err
	}
	if len(steps.Values) == 0 {
		return "", fmt.Errorf("no steps found for pipeline %s", id)
	}

	var b strings.Builder
	for i, st := range steps.Values {
		req, err := c.client.NewRequest("GET", fmt.Sprintf("repositories/%s/%s/pipelines/%s/steps/%s/log", workspace, slug, id, st.UUID), nil)
		if err != nil {
			return b.String(), err
		}
		req.Header.Set("Accept", "text/plain")
		resp, err := c.client.Do(req)
		if err != nil {
			return b.String(), err
		}
		if resp.StatusCode >= 400 {
			resp.Body.Close()
			fmt.Fprintf(&b, "\n--- step %d (%s): HTTP %d ---\n", i+1, st.Name, resp.StatusCode)
			continue
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return b.String(), err
		}
		fmt.Fprintf(&b, "\n=== step %d: %s ===\n", i+1, st.Name)
		b.Write(data)
	}
	return b.String(), nil
}

// --- Webhooks ---

type clWebhook struct {
	UUID        string   `json:"uuid"`
	Description string   `json:"description"`
	URL         string   `json:"url"`
	Events      []string `json:"events"`
	Active      bool     `json:"active"`
}

func (w clWebhook) toWebhook() Webhook {
	return Webhook{
		ID:          w.UUID,
		URL:         w.URL,
		Description: w.Description,
		Events:      w.Events,
		Active:      w.Active,
	}
}

func (c *cloudService) ListWebhooks(workspace, slug string) ([]Webhook, error) {
	var page clPaged[clWebhook]
	if err := c.client.getJSON(fmt.Sprintf("repositories/%s/%s/hooks", workspace, slug), &page); err != nil {
		return nil, err
	}
	out := make([]Webhook, 0, len(page.Values))
	for _, w := range page.Values {
		out = append(out, w.toWebhook())
	}
	return out, nil
}

func (c *cloudService) AddWebhook(workspace, slug string, in WebhookInput) (*Webhook, error) {
	desc := in.Description
	if desc == "" {
		desc = "bb-webhook"
	}
	body := map[string]any{
		"description": desc,
		"url":         in.URL,
		"events":      in.Events,
		"active":      in.Active,
	}
	var w clWebhook
	if err := c.client.postJSON(fmt.Sprintf("repositories/%s/%s/hooks", workspace, slug), body, &w); err != nil {
		return nil, err
	}
	out := w.toWebhook()
	return &out, nil
}

func (c *cloudService) DeleteWebhook(workspace, slug, id string) error {
	return c.client.deleteJSON(fmt.Sprintf("repositories/%s/%s/hooks/%s", workspace, slug, id))
}

func (c *cloudService) CreateRepo(in CreateRepoInput) (*Repo, error) {
	scm := in.SCM
	if scm == "" {
		scm = "git"
	}
	slug := in.Slug
	if slug == "" {
		slug = in.Name
	}
	body := map[string]any{"scm": scm, "is_private": in.Private}
	if in.Description != "" {
		body["description"] = in.Description
	}
	var r clRepo
	if err := c.client.postJSON(fmt.Sprintf("repositories/%s/%s", in.Project, slug), body, &r); err != nil {
		return nil, err
	}
	out := r.toRepo()
	return &out, nil
}

func (c *cloudService) TriggerPipeline(workspace, slug, ref string) (*Build, error) {
	if ref == "" {
		repo, err := c.GetRepo(workspace, slug)
		if err != nil {
			return nil, err
		}
		ref = repo.DefaultRef
	}
	body := map[string]any{
		"target": map[string]any{
			"type":     "pipeline_ref_target",
			"ref_type": "branch",
			"ref_name": ref,
		},
	}
	var p clPipeline
	if err := c.client.postJSON(fmt.Sprintf("repositories/%s/%s/pipelines/", workspace, slug), body, &p); err != nil {
		return nil, err
	}
	state := p.State.Name
	if p.State.Result.Name != "" {
		state = p.State.Result.Name
	}
	return &Build{
		ID:        fmt.Sprintf("#%d", p.BuildNumber),
		Name:      p.UUID,
		State:     state,
		URL:       p.Links.HTML.Href,
		Ref:       p.Target.RefName,
		Commit:    p.Target.Commit.Hash,
		CreatedAt: parseTime(p.CreatedOn),
	}, nil
}

func (c *cloudService) CancelPipeline(workspace, slug, idOrUUID string) error {
	id := idOrUUID
	if !strings.HasPrefix(id, "{") {
		// Numeric or '#N' — resolve via list.
		id = strings.TrimPrefix(id, "#")
	}
	return c.client.postJSON(fmt.Sprintf("repositories/%s/%s/pipelines/%s/stopPipeline", workspace, slug, id), nil, nil)
}

// SearchRepos searches within a workspace. Accepts "workspace" or
// "workspace/query" as the query string. With no slash, treats the
// whole input as a workspace and lists its repos. With a slash, lists
// the workspace's repos filtered by name~"query".
func (c *cloudService) SearchRepos(query string, limit int) ([]Repo, error) {
	if limit <= 0 {
		limit = 50
	}
	workspace := query
	name := ""
	if i := strings.Index(query, "/"); i > 0 {
		workspace = query[:i]
		name = query[i+1:]
	}
	if workspace == "" {
		workspace = c.client.cfg.Username
	}
	if workspace == "" {
		return nil, fmt.Errorf("provide a workspace (e.g. workspace or workspace/name)")
	}
	params := map[string]string{"pagelen": itoa(limit)}
	if name != "" {
		params["q"] = fmt.Sprintf(`name~"%s"`, name)
	}
	endpoint := fmt.Sprintf("repositories/%s%s", workspace, queryString(params))
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

// ListMyReviewPRs returns open PRs the configured user is involved in
// (Cloud's /pullrequests/{user} returns PRs the user authored or
// reviews; we filter to reviewer-role using participants).
func (c *cloudService) ListMyReviewPRs(limit int) ([]ReviewPR, error) {
	if limit <= 0 {
		limit = 50
	}
	user := c.client.cfg.Username
	if user == "" {
		return nil, fmt.Errorf("no username configured for host %s", c.host)
	}
	type clRepoLite struct {
		FullName string `json:"full_name"`
	}
	type clMyPR struct {
		clPR
		Destination struct {
			clBranch
			Repository clRepoLite `json:"repository"`
		} `json:"destination"`
	}
	endpoint := fmt.Sprintf("pullrequests/%s%s", user,
		queryString(map[string]string{"state": "OPEN", "pagelen": itoa(limit)}))
	var page clPaged[clMyPR]
	if err := c.client.getJSON(endpoint, &page); err != nil {
		return nil, err
	}
	out := make([]ReviewPR, 0, len(page.Values))
	for _, p := range page.Values {
		// Only keep PRs where the user appears as a REVIEWER (skip
		// pure authorship — those aren't "to review").
		isReviewer := false
		for _, pa := range p.Participants {
			if pa.Role == "REVIEWER" && (pa.User.Nickname == user || pa.User.UUID == user) {
				isReviewer = true
				break
			}
		}
		if !isReviewer && p.Author.Nickname == user {
			// Author-only — skip.
			continue
		}
		pr := p.clPR.toPR()
		ws, sl := splitFullName(p.Destination.Repository.FullName)
		out = append(out, ReviewPR{PR: pr, Project: ws, Slug: sl})
	}
	return out, nil
}

// ListMyAuthoredPRs is a no-op stub on Cloud — there is no equivalent
// dashboard endpoint and per-repo enumeration would be too slow.
func (c *cloudService) ListMyAuthoredPRs(limit int) ([]ReviewPR, error) {
	return []ReviewPR{}, nil
}

// ListRecentlyClosedPRs is a no-op stub on Cloud (see ListMyAuthoredPRs).
func (c *cloudService) ListRecentlyClosedPRs(limit int) ([]ReviewPR, error) {
	return []ReviewPR{}, nil
}

// ListRecentlyViewedRepos is a no-op stub on Cloud (no equivalent
// "recently viewed" endpoint exists in REST 2.0).
func (c *cloudService) ListRecentlyViewedRepos(limit int) ([]Repo, error) {
	return []Repo{}, nil
}

func splitFullName(s string) (ws, slug string) {
	if i := strings.Index(s, "/"); i > 0 {
		return s[:i], s[i+1:]
	}
	return "", s
}

// GetReadme fetches a README from the repo's default branch.
func (c *cloudService) GetReadme(workspace, slug string) (string, error) {
	repo, err := c.GetRepo(workspace, slug)
	if err != nil {
		return "", err
	}
	ref := repo.DefaultRef
	if ref == "" {
		ref = "main"
	}
	for _, name := range []string{"README.md", "README.MD", "Readme.md", "README", "README.txt"} {
		req, err := c.client.NewRequest("GET",
			fmt.Sprintf("repositories/%s/%s/src/%s/%s", workspace, slug, ref, name), nil)
		if err != nil {
			continue
		}
		req.Header.Set("Accept", "text/plain")
		resp, err := c.client.Do(req)
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

// SearchUsers is not yet implemented for Bitbucket Cloud — there's no
// directory-search endpoint that works without a workspace context,
// and the workspace is encoded into the project param of every other
// call rather than being available on the service. Callers should
// fall back to free-text reviewer entry on Cloud.
func (c *cloudService) SearchUsers(query string, limit int) ([]User, error) {
	return nil, nil
}

// ListTasks is a no-op on Bitbucket Cloud — the platform deprecated
// PR tasks in 2022 in favour of GFM checklists in the description.
// Returning an empty slice (rather than an error) lets the merge
// confirm view simply hide the "open tasks" section on Cloud.
func (c *cloudService) ListTasks(workspace, slug string, prID int) ([]Task, error) {
	return nil, nil
}

// ResolveTask is not supported on Bitbucket Cloud (see ListTasks).
func (c *cloudService) ResolveTask(workspace, slug string, prID, taskID int) error {
	return fmt.Errorf("resolving PR tasks is not supported on Bitbucket Cloud")
}
