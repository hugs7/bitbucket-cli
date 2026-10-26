package api

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

func (s *serverService) overrideRecoveryCommand(project, slug string) string {
	return fmt.Sprintf("bb pr recover-merge-checks -R %s/%s/%s", s.client.host, project, slug)
}

func overrideCount(v any) (int, bool) {
	n, ok := v.(float64)
	return int(n), ok && n >= 0 && n <= math.MaxInt32 && n == math.Trunc(n)
}

func overrideLocal(v map[string]any) bool {
	return overrideScope(v) == "REPOSITORY"
}

func overrideScope(v map[string]any) string {
	scope, _ := v["scope"].(map[string]any)
	id, ok := overrideCount(scope["resourceId"])
	if !ok || id == 0 {
		return ""
	}
	typ, _ := scope["type"].(string)
	return typ
}

func overrideCopy(v map[string]any) map[string]any {
	copy := make(map[string]any, len(v))
	for key, value := range v {
		copy[key] = value
	}
	return copy
}

func overrideConditionBody(c map[string]any, count any) map[string]any {
	return map[string]any{
		"sourceMatcher": c["sourceRefMatcher"], "targetMatcher": c["targetRefMatcher"],
		"reviewers": c["reviewers"], "requiredApprovals": count,
	}
}

func (s *serverService) overrideConditionPath(j *overrideJournal, c map[string]any) string {
	id, _ := overrideCount(c["id"])
	return s.overrideReviewerPath(j.Project, j.Slug) + "/condition/" + strconv.Itoa(id)
}

func overrideConditionMatches(c map[string]any, source, target string) (bool, error) {
	for _, pair := range []struct{ key, ref string }{{"targetRefMatcher", target}, {"sourceRefMatcher", source}} {
		matcher, ok := c[pair.key].(map[string]any)
		if !ok {
			return false, fmt.Errorf("missing default-reviewer %s", pair.key)
		}
		typ, _ := matcher["type"].(map[string]any)
		switch typ["id"] {
		case "ANY_REF":
			continue
		case "BRANCH":
			id, ok := matcher["id"].(string)
			if !ok || id == "" {
				return false, fmt.Errorf("missing default-reviewer branch matcher ID")
			}
			if strings.TrimPrefix(id, "refs/heads/") != strings.TrimPrefix(pair.ref, "refs/heads/") {
				return false, nil
			}
		default:
			// Do not approximate Bitbucket branch-model/pattern semantics. A
			// false negative could merge without disabling all requested checks.
			return false, fmt.Errorf("unsupported default-reviewer matcher %v; only ANY_REF and BRANCH can be safely bypassed", typ["id"])
		}
	}
	return true, nil
}

// overrideCollection accepts both the Westpac array response and standard
// paginated responses, following nextPageStart rather than assuming a page size.
func (s *serverService) overrideCollection(endpoint string) ([]map[string]any, error) {
	var result []map[string]any
	start := 0
	for {
		var raw json.RawMessage
		if err := s.client.getJSON(endpoint+"?limit=100&start="+strconv.Itoa(start), &raw); err != nil {
			return nil, err
		}
		var values []map[string]any
		if len(raw) > 0 && raw[0] == '[' {
			if err := json.Unmarshal(raw, &values); err != nil {
				return nil, err
			}
			return append(result, values...), nil
		}
		var page struct {
			Values        []map[string]any `json:"values"`
			IsLastPage    *bool            `json:"isLastPage"`
			NextPageStart *int             `json:"nextPageStart"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, err
		}
		if page.IsLastPage == nil || page.Values == nil {
			return nil, fmt.Errorf("incomplete settings collection at %s", endpoint)
		}
		result = append(result, page.Values...)
		if *page.IsLastPage {
			return result, nil
		}
		if page.NextPageStart == nil || *page.NextPageStart <= start {
			return nil, fmt.Errorf("invalid settings pagination at %s", endpoint)
		}
		start = *page.NextPageStart
	}
}
