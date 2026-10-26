package api

import (
	"fmt"
	"net/url"
	"strings"
)

// DefaultReviewers loads the applicable repository and inherited project defaults
// for a PR, excluding its author and duplicate identities. Server identities are
// login names; Cloud identities are UUIDs. Services can implement this optional
// capability without extending Service. Unsupported services return an error.
func DefaultReviewers(svc Service, project, slug, source, target string) ([]User, error) {
	provider, ok := svc.(interface {
		DefaultReviewers(project, slug, source, target string) ([]User, error)
	})
	if !ok {
		return nil, fmt.Errorf("loading default reviewers is not supported by this service")
	}
	users, err := provider.DefaultReviewers(project, slug, source, target)
	if err != nil {
		return nil, fmt.Errorf("load default reviewers for %s/%s (%s -> %s): %w", project, slug, source, target, err)
	}
	return users, nil
}

// DefaultReviewers asks Bitbucket to evaluate the source/target conditions,
// including inherited project rules, rather than merging configured conditions.
// https://docs.atlassian.com/bitbucket-server/rest/5.9.0/bitbucket-default-reviewers-rest.html
func (s *serverService) DefaultReviewers(project, slug, source, target string) ([]User, error) {
	if strings.TrimSpace(source) == "" || strings.TrimSpace(target) == "" {
		return nil, fmt.Errorf("source and target branches are required")
	}
	var repo struct {
		ID int `json:"id"`
	}
	if err := s.client.getJSON(fmt.Sprintf("projects/%s/repos/%s", url.PathEscape(project), url.PathEscape(slug)), &repo); err != nil {
		return nil, fmt.Errorf("resolve repository ID: %w", err)
	}
	if repo.ID <= 0 {
		return nil, fmt.Errorf("repository response has no valid ID")
	}
	endpoint := fmt.Sprintf("%s/rest/default-reviewers/1.0/projects/%s/repos/%s/reviewers%s",
		s.client.HostRoot(), url.PathEscape(project), url.PathEscape(slug), queryString(map[string]string{
			"sourceRepoId": itoa(repo.ID),
			"targetRepoId": itoa(repo.ID),
			"sourceRefId":  "refs/heads/" + strings.TrimPrefix(source, "refs/heads/"),
			"targetRefId":  "refs/heads/" + strings.TrimPrefix(target, "refs/heads/"),
		}))
	var raw []struct {
		srvUser
		Email string `json:"emailAddress"`
	}
	if err := s.client.getJSON(endpoint, &raw); err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, fmt.Errorf("default reviewers response is not a user list")
	}
	users := make([]User, 0, len(raw))
	for _, user := range raw {
		users = append(users, User{Username: user.Name, DisplayName: user.DisplayName, Email: user.Email})
	}
	return filterPRReviewers(users, s.Me())
}

// DefaultReviewers follows every effective-default-reviewers page. The endpoint
// returns nested users from both the repository and its project.
// https://developer.atlassian.com/cloud/bitbucket/rest/api-group-pullrequests/#api-repositories-workspace-repo-slug-effective-default-reviewers-get
func (c *cloudService) DefaultReviewers(workspace, slug, source, target string) ([]User, error) {
	endpoint := fmt.Sprintf("%s/repositories/%s/%s/effective-default-reviewers", c.client.baseURL(), url.PathEscape(workspace), url.PathEscape(slug))
	origin, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	users := []User{}
	seenPages := map[string]bool{}
	for endpoint != "" {
		if seenPages[endpoint] {
			return nil, fmt.Errorf("default reviewers pagination repeated a page")
		}
		seenPages[endpoint] = true
		var page clPaged[struct {
			User clUser `json:"user"`
		}]
		if err := c.client.getJSON(endpoint, &page); err != nil {
			return nil, err
		}
		if page.Values == nil {
			return nil, fmt.Errorf("default reviewers response has no values list")
		}
		for _, entry := range page.Values {
			users = append(users, User{Username: entry.User.UUID, DisplayName: entry.User.DisplayName})
		}
		if page.Next == "" {
			break
		}
		current, _ := url.Parse(endpoint)
		next, err := current.Parse(page.Next)
		if err != nil {
			return nil, fmt.Errorf("invalid default reviewers next page: %w", err)
		}
		if next.Scheme != origin.Scheme || next.Host != origin.Host || next.User != nil {
			return nil, fmt.Errorf("default reviewers next page points outside the API host")
		}
		endpoint = next.String()
	}
	return c.filterPRReviewers(users)
}

// createReviewers distinguishes omitted reviewers from an explicit empty list.
func (s *serverService) createReviewers(project, slug string, in CreatePRInput) ([]User, error) {
	if in.Reviewers == nil {
		return DefaultReviewers(s, project, slug, in.SourceRef, in.TargetRef)
	}
	return filterPRReviewers(reviewerUsers(in.Reviewers), s.Me())
}

// createReviewers resolves defaults before the single PR creation request.
func (c *cloudService) createReviewers(workspace, slug string, in CreatePRInput) ([]User, error) {
	if in.Reviewers == nil {
		return DefaultReviewers(c, workspace, slug, in.SourceRef, in.TargetRef)
	}
	users := reviewerUsers(in.Reviewers)
	for i := range users {
		name := strings.TrimSpace(users[i].Username)
		if name == "" {
			return nil, fmt.Errorf("reviewer has no user identity")
		}
		uuid, err := c.resolveUserUUID(name)
		if err != nil {
			return nil, fmt.Errorf("resolve PR reviewer: %w", err)
		}
		users[i].Username = uuid
	}
	return c.filterPRReviewers(users)
}

// reviewerUsers preserves explicit identities for the shared validation step.
func reviewerUsers(names []string) []User {
	users := make([]User, 0, len(names))
	for _, name := range names {
		users = append(users, User{Username: name})
	}
	return users
}

// filterPRReviewers resolves the authenticated Cloud UUID: the configured login
// may be an email or nickname and cannot safely be compared to reviewer UUIDs.
func (c *cloudService) filterPRReviewers(users []User) ([]User, error) {
	if len(users) == 0 {
		return users, nil
	}
	var author clUser
	if err := c.client.getJSON("user", &author); err != nil {
		return nil, fmt.Errorf("resolve PR author to exclude from reviewers: %w", err)
	}
	return filterPRReviewers(users, author.UUID)
}

// filterPRReviewers removes the author and duplicate identities, retaining order
// and metadata. Missing identities fail rather than silently dropping reviewers.
func filterPRReviewers(users []User, author string) ([]User, error) {
	if len(users) > 0 && strings.TrimSpace(author) == "" {
		return nil, fmt.Errorf("cannot determine PR author to exclude from reviewers")
	}
	seen := map[string]bool{strings.ToLower(strings.TrimSpace(author)): true}
	out := make([]User, 0, len(users))
	for _, user := range users {
		user.Username = strings.TrimSpace(user.Username)
		if user.Username == "" {
			return nil, fmt.Errorf("reviewer has no user identity")
		}
		key := strings.ToLower(user.Username)
		if !seen[key] {
			seen[key] = true
			out = append(out, user)
		}
	}
	return out, nil
}
