package api

import (
	"encoding/json"
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
	addWebhooksPanel(settings, c, workspace, slug)
	unsupportedSettingsPanels(settings, "Default branch", "Group permissions", "Merge strategies", "HTTP access tokens", "Audit log", "Jira issues", "Code Insights", "Reviewer groups", "Auto-decline", "Description template")

	fetchSettingsEndpoints(c.client, settings, []settingsEndpoint{
		{Panel: "Repository permissions", Hint: "Cloud repository user permissions", Path: fmt.Sprintf("workspaces/%s/permissions/repositories/%s", workspace, slug)},
		{Panel: "Branch permissions", Hint: "Cloud branch restrictions", Path: fmt.Sprintf("repositories/%s/%s/branch-restrictions", workspace, slug)},
		{Panel: "Access keys", Hint: "Cloud repository deploy keys", Path: fmt.Sprintf("repositories/%s/%s/deploy-keys", workspace, slug)},
		{Panel: "Push log", Hint: "Recent repository commits (Cloud has no dedicated push-log API)", Path: fmt.Sprintf("repositories/%s/%s/commits?pagelen=20", workspace, slug)},
		{Panel: "Branches", Hint: "Cloud repository branches", Path: fmt.Sprintf("repositories/%s/%s/refs/branches?pagelen=50", workspace, slug)},
		{Panel: "Hooks", Hint: "Cloud exposes repository hooks as webhooks", Path: fmt.Sprintf("repositories/%s/%s/hooks", workspace, slug)},
		{Panel: "Merge checks", Hint: "Cloud merge checks are represented by branch restrictions", Path: fmt.Sprintf("repositories/%s/%s/branch-restrictions", workspace, slug)},
		{Panel: "Default reviewers", Hint: "Cloud default reviewers", Path: fmt.Sprintf("repositories/%s/%s/default-reviewers", workspace, slug)},
		{Panel: "Required builds", Hint: "Cloud required build count branch restrictions", Path: fmt.Sprintf("repositories/%s/%s/branch-restrictions?q=%s", workspace, slug, url.QueryEscape("kind=\"require_passing_builds_to_merge\""))},
	})

	return settings, nil
}

func (s *serverService) RepoSettings(project, slug string) (*RepoSettings, error) {
	settings := newRepoSettings()

	addRepoPanel(settings, s, "Repository details", project, slug)
	addWebhooksPanel(settings, s, project, slug)
	unsupportedSettingsPanels(settings, "Push log", "Audit log", "Jira issues", "Code Insights", "Reviewer groups", "Description template", "HTTP access tokens")
	settings.Panels["HTTP access tokens"] = RepoSettingsPanel{Name: "HTTP access tokens", Hint: "Unsupported for safety: token listing/creation and secret management are never included in settings dumps. Use Bitbucket's dedicated token UI."}
	for name, resource := range map[string]string{
		"Merge checks": "pull-requests", "Merge strategies": "merge-strategies",
		"Default reviewers": "default-reviewers", "Auto-decline": "auto-decline",
		"Default branch": "default-branch", "Repository permissions": "permissions-users",
		"Group permissions": "permissions-groups", "Branch permissions": "branch-restrictions",
		"Access keys": "access-keys", "Hooks": "hooks",
	} {
		raw, err := ReadRepoSetting(s, project, slug, resource, "")
		hint := "Editable via documented REST API; admin permission and compatible server version required"
		if err != nil {
			settings.Panels[name] = RepoSettingsPanel{Name: name, Hint: hint, Error: err.Error()}
			continue
		}
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			settings.Panels[name] = RepoSettingsPanel{Name: name, Error: err.Error()}
			continue
		}
		panel := rawToSettingsPanel(name, hint, value)
		if resource == "pull-requests" {
			if count, err := RepoMinimumApprovals(raw); err == nil {
				panel.Hint += fmt.Sprintf(". Current minimum approving reviewers: %d (disabled plugin counts are not effective).", count)
			}
		}
		settings.Panels[name] = panel
	}
	settings.Panels["Required builds"] = RepoSettingsPanel{Name: "Required builds", Hint: "Read-only here. Edit the required-builds count in Merge checks. Named required-build policies and plugin checks are version-dependent and not managed by this client."}

	fetchSettingsEndpoints(s.client, settings, []settingsEndpoint{
		{Panel: "Branches", Hint: "Server repository branches", Path: fmt.Sprintf("projects/%s/repos/%s/branches?limit=50", project, slug)},
	})

	return settings, nil
}

func unsupportedSettingsPanels(settings *RepoSettings, names ...string) {
	for _, name := range names {
		settings.Panels[name] = RepoSettingsPanel{Name: name, Hint: "Unsupported in this client: no portable repository settings API implemented. Use Bitbucket's web UI; availability depends on version, plugins and permissions."}
	}
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
		if nested, ok := m[k].(map[string]any); ok {
			if k == "details" {
				fields = append(fields, RepoSettingsField{Name: "Hook key", Value: summarizeSettingValue(nested["key"])})
			}
			if k == "user" {
				fields = append(fields, RepoSettingsField{Name: "Username", Value: summarizeSettingValue(nested["name"])})
			}
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
