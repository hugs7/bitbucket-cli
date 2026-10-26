package api

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// RequiredApproversSetting is the documented Data Center merge-check fragment.
const RequiredApproversSetting = "com.atlassian.bitbucket.server.bundled-hooks.requiredApproversMergeHook"

// CanEditRepoSettings reports whether this backend implements the documented
// Data Center settings APIs. Permissions and installed versions are checked by
// the server, not inferred from this capability.
func CanEditRepoSettings(svc Service) bool {
	_, ok := svc.(*serverService)
	return ok
}

// ReadRepoSetting returns unabridged JSON for a supported setting. Resources are
// pull-requests, merge-strategies, auto-decline and default-reviewers. An empty
// reviewer ID lists all conditions; a supplied ID selects a repository condition.
// Cloud editing is deliberately unsupported rather than guessing equivalent APIs.
func ReadRepoSetting(svc Service, project, slug, resource, id string) (json.RawMessage, error) {
	if adminSetting(resource) {
		s, ok := svc.(*serverService)
		if !ok {
			return nil, fmt.Errorf("%s is supported for Server/Data Center only", resource)
		}
		return readAdminSetting(s, project, slug, resource, id)
	}
	s, path, err := repoSettingEndpoint(svc, project, slug, resource)
	if err != nil {
		return nil, err
	}
	if resource == "default-reviewers" {
		conditions, err := readSettingsCollection(s.client, path+"/conditions")
		if err != nil {
			return nil, err
		}
		if id == "" {
			return json.Marshal(conditions)
		}
		if err := validateConditionID(id); err != nil {
			return nil, err
		}
		for _, condition := range conditions {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(condition, &fields); err != nil {
				return nil, err
			}
			if string(fields["id"]) == id {
				if err := repositoryCondition(fields); err != nil {
					return nil, err
				}
				return condition, nil
			}
		}
		return nil, fmt.Errorf("repository default reviewer condition %s not found", id)
	}
	if id != "" {
		return nil, fmt.Errorf("--id is only valid for default-reviewers")
	}
	var raw json.RawMessage
	if err := s.client.getJSON(path, &raw); err != nil {
		return nil, fmt.Errorf("read %s (requires a supported Bitbucket version and permissions): %w", resource, err)
	}
	if resource == "merge-strategies" {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, err
		}
		if len(fields["mergeConfig"]) == 0 {
			return nil, fmt.Errorf("server did not return mergeConfig; merge strategy editing unavailable")
		}
		return fields["mergeConfig"], nil
	}
	return raw, nil
}

// ChangeRepoSetting applies an update, create or delete after the caller has
// confirmed it. Updates read current state and recursively overlay the JSON
// object, preserving omitted fields (arrays are replaced). An explicit empty
// object replaces that object, allowing mergeConfig inheritance to be restored.
// There is no atomic compare-and-swap API; concurrent administrators should
// coordinate changes. No write is retried or silently redirected to another API.
//
// Contracts: https://docs.atlassian.com/bitbucket-server/rest/7.21.0/bitbucket-rest.html
// and https://docs.atlassian.com/bitbucket-server/rest/7.21.0/bitbucket-default-reviewers-rest.html
func ChangeRepoSetting(svc Service, project, slug, resource, id, action string, patch json.RawMessage) error {
	if adminSetting(resource) {
		s, ok := svc.(*serverService)
		if !ok {
			return fmt.Errorf("%s editing is supported for Server/Data Center only", resource)
		}
		return changeAdminSetting(s, project, slug, resource, id, action, patch)
	}
	s, path, err := repoSettingEndpoint(svc, project, slug, resource)
	if err != nil {
		return err
	}
	if resource != "default-reviewers" && id != "" {
		return fmt.Errorf("--id is only valid for default-reviewers")
	}
	if resource == "default-reviewers" && action != "create" {
		if err := validateConditionID(id); err != nil {
			return err
		}
	}
	if action == "delete" {
		if resource != "default-reviewers" && resource != "auto-decline" {
			return fmt.Errorf("delete is only supported for default-reviewers and auto-decline")
		}
		if resource == "default-reviewers" {
			if _, err := ReadRepoSetting(svc, project, slug, resource, id); err != nil {
				return err
			}
			path += "/condition/" + id
		}
		return s.client.deleteJSON(path)
	}
	if action != "update" && action != "create" {
		return fmt.Errorf("unknown settings action %q", action)
	}
	fields, err := settingObject(patch)
	if err != nil {
		return err
	}
	if action == "create" {
		if resource != "default-reviewers" || id != "" {
			return fmt.Errorf("create is only supported for default-reviewers without --id")
		}
	} else {
		current, err := ReadRepoSetting(svc, project, slug, resource, id)
		if err != nil {
			return err
		}
		base, err := settingObject(current)
		if err != nil {
			return err
		}
		// Responses use sourceRefMatcher/targetRefMatcher; writes use
		// sourceMatcher/targetMatcher. Normalize before overlaying the patch.
		if resource == "default-reviewers" {
			normalizeCondition(base)
			normalizeCondition(fields)
		}
		if resource == "pull-requests" {
			if _, unsafe := fields[WestpacRequiredApproversSetting]; unsafe {
				return fmt.Errorf("the Westpac required-approvers plugin object is response-only here; update requiredApprovers or use --minimum-approvals")
			}
			if _, westpac := base[WestpacRequiredApproversSetting]; westpac {
				if _, modern := fields[RequiredApproversSetting]; modern {
					return fmt.Errorf("this server uses requiredApprovers scalar writes; do not submit a plugin object")
				}
			}
			// This endpoint explicitly supports partial fragments. Do not
			// resubmit untouched settings, especially inherited mergeConfig.
			for key := range base {
				if _, changed := fields[key]; !changed {
					delete(base, key)
				}
			}
		}
		// Empty merge strategies explicitly restores inherited configuration.
		if resource != "merge-strategies" || len(fields) != 0 {
			fields = overlaySetting(base, fields)
		}
	}
	method := "POST"
	switch resource {
	case "default-reviewers":
		normalizeCondition(fields)
		for _, name := range []string{"sourceMatcher", "targetMatcher", "reviewers", "requiredApprovals"} {
			if len(fields[name]) == 0 || string(fields[name]) == "null" {
				return fmt.Errorf("default reviewer condition requires %s", name)
			}
		}
		delete(fields, "id")
		delete(fields, "scope")
		path += "/condition"
		if action == "update" {
			method = "PUT"
			path += "/" + id
		}
	case "auto-decline":
		delete(fields, "scope")
		method = "PUT"
	case "merge-strategies":
		if err := normalizeMergeConfig(fields); err != nil {
			return err
		}
		raw, err := json.Marshal(fields)
		if err != nil {
			return err
		}
		fields = map[string]json.RawMessage{"mergeConfig": raw}
	}
	if resource == "pull-requests" {
		// Avoid submitting conflicting deprecated aliases alongside the
		// modern fragment when editing the minimum approvals/build counts.
		for legacy, modern := range map[string]string{
			"requiredApprovers":        RequiredApproversSetting,
			"requiredSuccessfulBuilds": "com.atlassian.bitbucket.server.bitbucket-build.requiredBuildsMergeCheck",
		} {
			var changed map[string]json.RawMessage
			_ = json.Unmarshal(patch, &changed)
			if _, ok := changed[modern]; ok {
				if _, conflict := changed[legacy]; conflict {
					return fmt.Errorf("specify either %s or %s, not both", legacy, modern)
				}
			}
		}
		if raw, exists := fields["mergeConfig"]; exists {
			merge, err := settingObject(raw)
			if err != nil {
				return err
			}
			if err := normalizeMergeConfig(merge); err != nil {
				return err
			}
			fields["mergeConfig"], _ = json.Marshal(merge)
		}
	}
	if method == "PUT" {
		err = s.client.putJSON(path, fields, nil)
	} else {
		err = s.client.postJSON(path, fields, nil)
	}
	if err != nil {
		return fmt.Errorf("%s %s: %w", action, resource, err)
	}
	return nil
}

func repoSettingEndpoint(svc Service, project, slug, resource string) (*serverService, string, error) {
	s, ok := svc.(*serverService)
	if !ok {
		return nil, "", fmt.Errorf("repository settings editing is supported for Bitbucket Server/Data Center only; Cloud panels are read-only")
	}
	base := "projects/" + url.PathEscape(project) + "/repos/" + url.PathEscape(slug)
	switch resource {
	case "pull-requests", "merge-strategies":
		return s, base + "/settings/pull-requests", nil
	case "auto-decline":
		return s, base + "/settings/auto-decline", nil
	case "default-reviewers":
		return s, s.client.HostRoot() + "/rest/default-reviewers/latest/" + base, nil
	default:
		return nil, "", fmt.Errorf("unsupported settings resource %q (use pull-requests, merge-strategies, default-reviewers or auto-decline)", resource)
	}
}

func readSettingsCollection(client *Client, path string) ([]json.RawMessage, error) {
	conditions := []json.RawMessage{}
	start := 0
	for {
		var raw json.RawMessage
		if err := client.getJSON(fmt.Sprintf("%s?limit=100&start=%d", path, start), &raw); err != nil {
			return nil, err
		}
		// Older default-reviewers versions return a bare array.
		var array []json.RawMessage
		if json.Unmarshal(raw, &array) == nil && array != nil {
			return append(conditions, array...), nil
		}
		var page struct {
			Values []json.RawMessage `json:"values"`
			Last   *bool             `json:"isLastPage"`
			Next   int               `json:"nextPageStart"`
		}
		if err := json.Unmarshal(raw, &page); err != nil || page.Values == nil {
			return nil, fmt.Errorf("invalid settings collection response")
		}
		conditions = append(conditions, page.Values...)
		if page.Last == nil || *page.Last {
			return conditions, nil
		}
		if page.Next <= start {
			return nil, fmt.Errorf("invalid settings pagination: nextPageStart did not advance")
		}
		start = page.Next
	}
}

func settingObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, fmt.Errorf("settings must be a JSON object")
	}
	return fields, nil
}

func overlaySetting(base, patch map[string]json.RawMessage) map[string]json.RawMessage {
	for key, value := range patch {
		child, err := settingObject(value)
		old, oldErr := settingObject(base[key])
		if err == nil && oldErr == nil && len(child) > 0 {
			value, _ = json.Marshal(overlaySetting(old, child))
		}
		base[key] = value
	}
	return base
}

func normalizeCondition(fields map[string]json.RawMessage) {
	for _, prefix := range []string{"source", "target"} {
		if value, ok := fields[prefix+"RefMatcher"]; ok {
			if _, exists := fields[prefix+"Matcher"]; !exists {
				fields[prefix+"Matcher"] = value
			}
			delete(fields, prefix+"RefMatcher")
		}
	}
}

func normalizeMergeConfig(fields map[string]json.RawMessage) error {
	delete(fields, "type") // response-only inheritance metadata
	if raw, exists := fields["strategies"]; exists {
		var strategies []map[string]json.RawMessage
		if err := json.Unmarshal(raw, &strategies); err != nil || strategies == nil {
			return fmt.Errorf("merge strategies must be an array of strategy objects")
		}
		enabled := make([]map[string]json.RawMessage, 0, len(strategies))
		for _, strategy := range strategies {
			// POST enables every strategy included, irrespective of the GET
			// representation's enabled flag. Never enable disabled strategies
			// merely because another mergeConfig field was edited.
			if string(strategy["enabled"]) != "false" {
				enabled = append(enabled, strategy)
			}
		}
		fields["strategies"], _ = json.Marshal(enabled)
	}
	return nil
}

func repositoryCondition(fields map[string]json.RawMessage) error {
	var scope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(fields["scope"], &scope); err != nil || scope.Type != "REPOSITORY" {
		return fmt.Errorf("condition is inherited or has unknown scope; edit it at its owning project instead")
	}
	return nil
}

func validateConditionID(id string) error {
	n, err := strconv.Atoi(id)
	if err != nil || n < 0 || strconv.Itoa(n) != id {
		return fmt.Errorf("a numeric --id is required for a repository default reviewer condition")
	}
	return nil
}
