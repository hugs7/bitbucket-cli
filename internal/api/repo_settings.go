package api

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
)

type settingsEndpoint struct {
	Panel string
	Hint  string
	Path  string
}

func (c *cloudService) RepoSettings(workspace, slug string) (*RepoSettings, error) {
	settings := newRepoSettings()

	addRepoPanel(settings, c, "Repository details", workspace, slug)
	addMergeStrategiesPanel(settings, c, workspace, slug)
	addWebhooksPanel(settings, c, workspace, slug)

	fetchSettingsEndpoints(c.client, settings, []settingsEndpoint{
		{Panel: "Repository permissions", Hint: "Cloud repository user permissions", Path: fmt.Sprintf("workspaces/%s/permissions/repositories/%s", workspace, slug)},
		{Panel: "Branch permissions", Hint: "Cloud branch restrictions", Path: fmt.Sprintf("repositories/%s/%s/branch-restrictions", workspace, slug)},
		{Panel: "Access keys", Hint: "Cloud repository deploy keys", Path: fmt.Sprintf("repositories/%s/%s/deploy-keys", workspace, slug)},
		{Panel: "HTTP access tokens", Hint: "Cloud repository access tokens", Path: fmt.Sprintf("repositories/%s/%s/access-tokens", workspace, slug)},
		{Panel: "Push log", Hint: "Recent repository commits (Cloud has no dedicated push-log API)", Path: fmt.Sprintf("repositories/%s/%s/commits?pagelen=20", workspace, slug)},
		{Panel: "Audit log", Hint: "Cloud workspace audit events filtered by repository when available", Path: fmt.Sprintf("workspaces/%s/auditlog?q=%s", workspace, url.QueryEscape(fmt.Sprintf("repository.full_name=\"%s/%s\"", workspace, slug)))},
		{Panel: "Branches", Hint: "Cloud repository branches", Path: fmt.Sprintf("repositories/%s/%s/refs/branches?pagelen=50", workspace, slug)},
		{Panel: "Hooks", Hint: "Cloud exposes repository hooks as webhooks", Path: fmt.Sprintf("repositories/%s/%s/hooks", workspace, slug)},
		{Panel: "Jira issues", Hint: "Cloud repository issues; Jira-linked issues may require Jira APIs", Path: fmt.Sprintf("repositories/%s/%s/issues?pagelen=20", workspace, slug)},
		{Panel: "Merge checks", Hint: "Cloud merge checks are represented by branch restrictions", Path: fmt.Sprintf("repositories/%s/%s/branch-restrictions", workspace, slug)},
		{Panel: "Code Insights", Hint: "Cloud Code Insights reports for the default branch tip require commit-specific APIs", Path: fmt.Sprintf("repositories/%s/%s/commit/HEAD/reports", workspace, slug)},
		{Panel: "Default reviewers", Hint: "Cloud default reviewers", Path: fmt.Sprintf("repositories/%s/%s/default-reviewers", workspace, slug)},
		{Panel: "Reviewer groups", Hint: "Cloud does not expose repository reviewer groups as a first-class REST collection", Path: fmt.Sprintf("repositories/%s/%s/default-reviewers", workspace, slug)},
		{Panel: "Auto-decline", Hint: "Cloud auto-decline settings when exposed for this workspace", Path: fmt.Sprintf("repositories/%s/%s/settings/auto-decline", workspace, slug)},
		{Panel: "Description template", Hint: "Cloud pull request description template when exposed for this workspace", Path: fmt.Sprintf("repositories/%s/%s/settings/pullrequests/description-template", workspace, slug)},
		{Panel: "Required builds", Hint: "Cloud required build count branch restrictions", Path: fmt.Sprintf("repositories/%s/%s/branch-restrictions?q=%s", workspace, slug, url.QueryEscape("kind=\"require_passing_builds_to_merge\""))},
	})

	return settings, nil
}

func (s *serverService) RepoSettings(project, slug string) (*RepoSettings, error) {
	settings := newRepoSettings()

	addRepoPanel(settings, s, "Repository details", project, slug)
	addMergeStrategiesPanel(settings, s, project, slug)
	addWebhooksPanel(settings, s, project, slug)

	root := strings.TrimRight(s.client.HostRoot(), "/")
	fetchSettingsEndpoints(s.client, settings, []settingsEndpoint{
		{Panel: "Repository permissions", Hint: "Server repository user permissions", Path: fmt.Sprintf("projects/%s/repos/%s/permissions/users?limit=100", project, slug)},
		{Panel: "Branch permissions", Hint: "Server branch permission restrictions", Path: fmt.Sprintf("%s/rest/branch-permissions/2.0/projects/%s/repos/%s/restrictions?limit=100", root, project, slug)},
		{Panel: "Access keys", Hint: "Server repository SSH access keys", Path: fmt.Sprintf("%s/rest/ssh/latest/projects/%s/repos/%s/keys?limit=100", root, project, slug)},
		{Panel: "HTTP access tokens", Hint: "Server repository HTTP access tokens", Path: fmt.Sprintf("%s/rest/access-tokens/latest/projects/%s/repos/%s?limit=100", root, project, slug)},
		{Panel: "Push log", Hint: "Server audit events for repository pushes when audit REST is enabled", Path: fmt.Sprintf("%s/rest/audit/latest/projects/%s/repos/%s/events?limit=50&filter=repo-push", root, project, slug)},
		{Panel: "Audit log", Hint: "Server repository audit events", Path: fmt.Sprintf("%s/rest/audit/latest/projects/%s/repos/%s/events?limit=50", root, project, slug)},
		{Panel: "Branches", Hint: "Server repository branches", Path: fmt.Sprintf("projects/%s/repos/%s/branches?limit=50", project, slug)},
		{Panel: "Hooks", Hint: "Server repository hooks", Path: fmt.Sprintf("projects/%s/repos/%s/settings/hooks?limit=100", project, slug)},
		{Panel: "Jira issues", Hint: "Server Jira-linked issue keys from recent commits when integration APIs are unavailable", Path: fmt.Sprintf("projects/%s/repos/%s/commits?limit=25", project, slug)},
		{Panel: "Merge checks", Hint: "Server pull-request merge checks", Path: fmt.Sprintf("projects/%s/repos/%s/settings/pull-requests", project, slug)},
		{Panel: "Code Insights", Hint: "Server Code Insights reports require commit-specific APIs", Path: fmt.Sprintf("projects/%s/repos/%s/commits?limit=10", project, slug)},
		{Panel: "Default reviewers", Hint: "Server default reviewer conditions", Path: fmt.Sprintf("%s/rest/default-reviewers/latest/projects/%s/repos/%s/conditions?limit=100", root, project, slug)},
		{Panel: "Reviewer groups", Hint: "Server reviewer group settings when supported by this Bitbucket version", Path: fmt.Sprintf("projects/%s/repos/%s/settings/pull-requests/reviewer-groups", project, slug)},
		{Panel: "Auto-decline", Hint: "Server auto-decline settings when supported by this Bitbucket version", Path: fmt.Sprintf("projects/%s/repos/%s/settings/pull-requests/auto-decline", project, slug)},
		{Panel: "Description template", Hint: "Server pull request description template when supported by this Bitbucket version", Path: fmt.Sprintf("projects/%s/repos/%s/settings/pull-requests/description-template", project, slug)},
		{Panel: "Required builds", Hint: "Server required builds / build merge checks", Path: fmt.Sprintf("projects/%s/repos/%s/settings/pull-requests/required-builds", project, slug)},
	})

	return settings, nil
}

func fetchSettingsEndpoints(client *Client, settings *RepoSettings, endpoints []settingsEndpoint) {
	type result struct {
		panel string
		data  RepoSettingsPanel
	}
	results := make(chan result, len(endpoints))
	for _, ep := range endpoints {
		ep := ep
		go func() {
			results <- result{panel: ep.Panel, data: fetchSettingsPanel(client, ep)}
		}()
	}
	for range endpoints {
		res := <-results
		settings.Panels[res.panel] = res.data
	}
}

func newRepoSettings() *RepoSettings {
	return &RepoSettings{Panels: map[string]RepoSettingsPanel{}}
}

func addRepoPanel(settings *RepoSettings, svc Service, name, project, slug string) {
	r, err := svc.GetRepo(project, slug)
	if err != nil {
		settings.Panels[name] = RepoSettingsPanel{Name: name, Error: err.Error()}
		return
	}
	fields := []RepoSettingsField{
		{Name: "Project/workspace", Value: r.Project},
		{Name: "Slug", Value: r.Slug},
		{Name: "Name", Value: r.Name},
		{Name: "Default branch", Value: r.DefaultRef},
		{Name: "Web URL", Value: r.WebURL},
		{Name: "Clone HTTPS", Value: r.CloneHTTPS},
		{Name: "Clone SSH", Value: r.CloneSSH},
		{Name: "Description", Value: r.Description},
	}
	settings.Panels[name] = RepoSettingsPanel{
		Name:  name,
		Hint:  "Repository metadata",
		Items: []RepoSettingsItem{{Title: r.Project + "/" + r.Slug, Fields: compactFields(fields)}},
	}
}

func addMergeStrategiesPanel(settings *RepoSettings, svc Service, project, slug string) {
	strategies, err := svc.MergeStrategies(project, slug)
	name := "Merge strategies"
	if err != nil {
		settings.Panels[name] = RepoSettingsPanel{Name: name, Hint: "Allowed pull-request merge strategies", Error: err.Error()}
		return
	}
	items := make([]RepoSettingsItem, 0, len(strategies))
	for _, st := range strategies {
		fields := []RepoSettingsField{{Name: "ID", Value: st.ID}}
		if st.Default {
			fields = append(fields, RepoSettingsField{Name: "Default", Value: "yes"})
		}
		items = append(items, RepoSettingsItem{Title: st.Name, Fields: fields})
	}
	settings.Panels[name] = RepoSettingsPanel{Name: name, Hint: "Allowed pull-request merge strategies", Items: items}
}

func addWebhooksPanel(settings *RepoSettings, svc Service, project, slug string) {
	hooks, err := svc.ListWebhooks(project, slug)
	name := "Webhooks"
	if err != nil {
		settings.Panels[name] = RepoSettingsPanel{Name: name, Hint: "Repository webhooks", Error: err.Error()}
		return
	}
	items := make([]RepoSettingsItem, 0, len(hooks))
	for _, hook := range hooks {
		items = append(items, RepoSettingsItem{
			Title: firstNonEmpty(hook.Description, hook.ID),
			Fields: compactFields([]RepoSettingsField{
				{Name: "ID", Value: hook.ID},
				{Name: "URL", Value: hook.URL},
				{Name: "Events", Value: strings.Join(hook.Events, ", ")},
				{Name: "Active", Value: yesNo(hook.Active)},
			}),
		})
	}
	settings.Panels[name] = RepoSettingsPanel{Name: name, Hint: "Repository webhooks", Items: items}
}

func fetchSettingsPanel(client *Client, ep settingsEndpoint) RepoSettingsPanel {
	var raw any
	if err := client.getJSON(ep.Path, &raw); err != nil {
		return RepoSettingsPanel{Name: ep.Panel, Hint: ep.Hint, Error: err.Error()}
	}
	return rawToSettingsPanel(ep.Panel, ep.Hint, raw)
}

func rawToSettingsPanel(name, hint string, raw any) RepoSettingsPanel {
	items := rawToSettingsItems(raw)
	return RepoSettingsPanel{Name: name, Hint: hint, Items: items}
}

func rawToSettingsItems(raw any) []RepoSettingsItem {
	if raw == nil {
		return nil
	}
	if m, ok := raw.(map[string]any); ok {
		if values, ok := m["values"].([]any); ok {
			return rawSliceToItems(values)
		}
		return []RepoSettingsItem{mapToSettingsItem(m)}
	}
	if values, ok := raw.([]any); ok {
		return rawSliceToItems(values)
	}
	return []RepoSettingsItem{{Title: summarizeSettingValue(raw)}}
}

func rawSliceToItems(values []any) []RepoSettingsItem {
	items := make([]RepoSettingsItem, 0, len(values))
	for _, v := range values {
		if m, ok := v.(map[string]any); ok {
			items = append(items, mapToSettingsItem(m))
			continue
		}
		items = append(items, RepoSettingsItem{Title: summarizeSettingValue(v)})
	}
	return items
}

func mapToSettingsItem(m map[string]any) RepoSettingsItem {
	fields := make([]RepoSettingsField, 0, len(m))
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k == "links" || k == "properties" {
			continue
		}
		value := summarizeSettingValue(m[k])
		if value == "" {
			continue
		}
		fields = append(fields, RepoSettingsField{Name: humanizeSettingKey(k), Value: value})
	}
	return RepoSettingsItem{Title: titleFromFields(fields), Fields: fields}
}

func summarizeSettingValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return yesNo(x)
	case float64:
		return fmt.Sprintf("%g", x)
	case []any:
		if len(x) == 0 {
			return ""
		}
		parts := make([]string, 0, len(x))
		for _, item := range x {
			parts = append(parts, summarizeSettingValue(item))
		}
		return strings.Join(compactStrings(parts), ", ")
	case map[string]any:
		for _, key := range []string{"display_name", "displayName", "name", "username", "slug", "id", "key", "kind", "pattern", "type", "value"} {
			if value := summarizeSettingValue(x[key]); value != "" {
				return value
			}
		}
		parts := make([]string, 0, len(x))
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if k == "links" || k == "properties" {
				continue
			}
			if value := summarizeSettingValue(x[k]); value != "" {
				parts = append(parts, humanizeSettingKey(k)+"="+value)
			}
		}
		return strings.Join(parts, ", ")
	default:
		return fmt.Sprint(x)
	}
}

func titleFromFields(fields []RepoSettingsField) string {
	for _, want := range []string{"Name", "Display name", "Display id", "Username", "Permission", "Kind", "Pattern", "Id", "Key", "Slug", "Title"} {
		for _, f := range fields {
			if strings.EqualFold(f.Name, want) && f.Value != "" {
				return f.Value
			}
		}
	}
	if len(fields) > 0 {
		return fields[0].Value
	}
	return "setting"
}

func compactFields(fields []RepoSettingsField) []RepoSettingsField {
	out := fields[:0]
	for _, f := range fields {
		if strings.TrimSpace(f.Value) != "" {
			out = append(out, f)
		}
	}
	return out
}

func compactStrings(in []string) []string {
	out := in[:0]
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

func humanizeSettingKey(s string) string {
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "-", " ")
	parts := strings.Fields(s)
	for i, p := range parts {
		if len(p) == 0 {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
